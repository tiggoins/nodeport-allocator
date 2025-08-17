package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/tiggoins/nodeport-allocator/pkg/admission"
	"github.com/tiggoins/nodeport-allocator/pkg/config"
	"github.com/tiggoins/nodeport-allocator/pkg/controller"
	"github.com/tiggoins/nodeport-allocator/pkg/leader"
	"github.com/tiggoins/nodeport-allocator/pkg/portmanager"
	"github.com/tiggoins/nodeport-allocator/pkg/utils"
	"github.com/tiggoins/nodeport-allocator/pkg/webhook"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
}

func main() {
	var configFile string
	var enableLeaderElection bool
	var metricsAddr string
	var probeAddr string
	var webhookPort int
	var webhookCertDir string

	flag.StringVar(&configFile, "config", "config/config.yaml", "配置文件路径")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false, "启用 Leader Election")
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "metrics 服务地址")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "健康检查服务地址")
	flag.IntVar(&webhookPort, "webhook-port", 9443, "webhook 服务端口")
	flag.StringVar(&webhookCertDir, "webhook-cert-dir", "/tmp/k8s-webhook-server/serving-certs", "webhook 证书目录")
	flag.Parse()

	opts := zap.Options{
		Development: true,
	}
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// 加载配置
	cfg, err := config.LoadConfig(configFile)
	if err != nil {
		setupLog.Error(err, "加载配置失败")
		os.Exit(1)
	}

	// 创建 Manager
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   webhookPort,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "nodeport-allocator-leader",
		CertDir:                webhookCertDir,
	})
	if err != nil {
		setupLog.Error(err, "创建 manager 失败")
		os.Exit(1)
	}

	ctx := setupSignalHandler()
	// 创建端口管理器
	portManager, err := portmanager.NewManager(ctx, mgr.GetClient(), cfg, utils.NewLogger("portmanager"))
	if err != nil {
		setupLog.Error(err, "创建端口管理器失败")
		os.Exit(1)
	}

	if err := portManager.Initialize(ctx); err != nil {
		setupLog.Error(err, "初始化端口管理器失败")
		os.Exit(1)
	}

	// 扫描现有的NodePort Services并初始化端口状态
	if err := portManager.ScanExistingServices(ctx); err != nil {
		setupLog.Error(err, "扫描现有NodePort Services失败")
		os.Exit(1)
	}

	// 设置 webhook
	webhookServer := mgr.GetWebhookServer()
	mutator := admission.NewMutator(portManager, utils.NewLogger("mutator"))
	webhookServer.Register("/mutate", &webhook.AdmissionHandler{
		Handler: mutator,
		Logger:  utils.NewLogger("webhook"),
	})

	// 设置控制器（仅在 Leader 模式下运行）
	if enableLeaderElection {
		if err = setupControllerWithLeaderElection(mgr, portManager); err != nil {
			setupLog.Error(err, "设置控制器失败")
			os.Exit(1)
		}
	} else {
		if err = setupController(mgr, portManager); err != nil {
			setupLog.Error(err, "设置控制器失败")
			os.Exit(1)
		}
	}

	// 添加健康检查
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "添加健康检查失败")
		os.Exit(1)
	}

	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "添加就绪检查失败")
		os.Exit(1)
	}

	setupLog.Info("启动 manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "manager 运行出错")
		os.Exit(1)
	}
}

func setupController(mgr manager.Manager, portManager *portmanager.Manager) error {
	return (&controller.ServiceReconciler{
		Client:      mgr.GetClient(),
		Scheme:      mgr.GetScheme(),
		PortManager: portManager,
		Logger:      utils.NewLogger("controller"),
	}).SetupWithManager(mgr)
}

func setupControllerWithLeaderElection(mgr manager.Manager, portManager *portmanager.Manager) error {
	// 从 controller-runtime manager 拿底层的 *rest.Config
	cfg := mgr.GetConfig()

	// 用 client-go 构造 clientset
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to create clientset: %w", err)
	}
	leaderElection := leader.NewElection(
		clientset,
		"nodeport-allocator-controller",
		func(ctx context.Context) {
			if err := setupController(mgr, portManager); err != nil {
				setupLog.Error(err, "Leader 模式下设置控制器失败")
			}
		},
		utils.NewLogger("leader"),
	)

	return mgr.Add(leaderElection)
}

func setupSignalHandler() context.Context {
	ctx, cancel := context.WithCancel(context.Background())

	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		setupLog.Info("收到终止信号，正在关闭...")
		cancel()
		<-c
		os.Exit(1) // 强制退出
	}()

	return ctx
}
