package k8s

import (
	"context"
	"flag"
	"os"
	"os/exec"
	"time"

	"github.com/convox/convox/pkg/atom"
	"github.com/convox/convox/pkg/common"
	"github.com/convox/convox/pkg/manifest"
	"github.com/convox/convox/pkg/structs"
	"github.com/convox/convox/pkg/templater"
	"github.com/convox/logger"
	"github.com/gobuffalo/packr"
	am "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type Engine interface {
	AppIdles(app string) (bool, error)
	AppStatus(app string) (string, error)
	Log(app, stream string, ts time.Time, message string) error
	ReleasePromote(app, id string, opts structs.ReleasePromoteOptions) error
	RepositoryAuth(app string) (string, string, error)
	RepositoryHost(app string) (string, bool, error)
	ResourceRender(app string, r manifest.Resource) ([]byte, error)
	Resolver() (string, error)
	ServiceHost(app string, s manifest.Service) string
	// SystemAnnotations(service string) map[string]string
	SystemHost() string
	SystemStatus() (string, error)
}

type Provider struct {
	Config  *rest.Config
	Cluster kubernetes.Interface
	Domain  string
	// ID        string
	Engine Engine
	Image  string
	// Metrics   metrics.Interface
	Name      string
	Namespace string
	Password  string
	Provider  string
	Socket    string
	Storage   string
	Version   string

	atom      *atom.Client
	ctx       context.Context
	logger    *logger.Logger
	templater *templater.Templater
}

func FromEnv() (*Provider, error) {
	// hack to make glog stop complaining about flag parsing
	fs := flag.NewFlagSet("", flag.ContinueOnError)
	_ = fs.Parse([]string{})
	flag.CommandLine = fs
	runtime.ErrorHandlers = []func(error){}

	namespace := os.Getenv("NAMESPACE")

	c, err := restConfig()
	if err != nil {
		return nil, err
	}

	ac, err := atom.New(c)
	if err != nil {
		return nil, err
	}

	kc, err := kubernetes.NewForConfig(c)
	if err != nil {
		return nil, err
	}

	ns, err := kc.CoreV1().Namespaces().Get(namespace, am.GetOptions{})
	if err != nil {
		return nil, err
	}

	p := &Provider{
		Config:    c,
		Cluster:   kc,
		Domain:    os.Getenv("DOMAIN"),
		Image:     os.Getenv("IMAGE"),
		Name:      ns.Labels["rack"],
		Namespace: ns.Name,
		Password:  os.Getenv("PASSWORD"),
		Provider:  common.CoalesceString(os.Getenv("PROVIDER"), "k8s"),
		Socket:    common.CoalesceString(os.Getenv("SOCKET"), "/var/run/docker.sock"),
		Storage:   common.CoalesceString(os.Getenv("STORAGE"), "/var/storage"),
		Version:   common.CoalesceString(os.Getenv("VERSION"), "dev"),
		atom:      ac,
		ctx:       context.Background(),
		logger:    logger.New("ns=k8s"),
	}

	p.templater = templater.New(packr.NewBox("../k8s/template"), p.templateHelpers())

	return p, nil
}

func restConfig() (*rest.Config, error) {
	if c, err := rest.InClusterConfig(); err == nil {
		return c, nil
	}

	data, err := exec.Command("kubectl", "config", "view", "--raw").CombinedOutput()
	if err != nil {
		return nil, err
	}

	cfg, err := clientcmd.NewClientConfigFromBytes(data)
	if err != nil {
		return nil, err
	}

	c, err := cfg.ClientConfig()
	if err != nil {
		return nil, err
	}

	return c, nil
}

// func FromEnv() (*Provider, error) {
// 	// hack to make glog stop complaining about flag parsing
// 	fs := flag.NewFlagSet("", flag.ContinueOnError)
// 	_ = fs.Parse([]string{})
// 	flag.CommandLine = fs
// 	runtime.ErrorHandlers = []func(error){}

// 	p := &Provider{
// 		ID:       os.Getenv("ID"),
// 		Image:    os.Getenv("IMAGE"),
// 		Password: os.Getenv("PASSWORD"),
// 		Provider: os.Getenv("PROVIDER"),
// 		Rack:     os.Getenv("RACK"),
// 		Socket:   common.CoalesceString(os.Getenv("SOCKET"), "/var/run/docker.sock"),
// 		Storage:  os.Getenv("STORAGE"),
// 		Version:  os.Getenv("VERSION"),
// 		ctx:      context.Background(),
// 		logger:   logger.Discard,
// 	}

// 	p.templater = templater.New(packr.NewBox("../k8s/template"), p.templateHelpers())

// 	if cfg, err := rest.InClusterConfig(); err == nil {
// 		p.logger = logger.New("ns=k8s")

// 		p.Config = cfg

// 		ac, err := atom.New(cfg)
// 		if err != nil {
// 			return nil, err
// 		}

// 		p.atom = ac

// 		kc, err := kubernetes.NewForConfig(cfg)
// 		if err != nil {
// 			return nil, err
// 		}

// 		p.Cluster = kc

// 		mc, err := metrics.NewForConfig(cfg)
// 		if err != nil {
// 			return nil, err
// 		}

// 		p.Metrics = mc
// 	}

// 	if p.ID == "" {
// 		p.ID, _ = dockerSystemId()
// 	}

// 	return p, nil
// }

func (p *Provider) Initialize(opts structs.ProviderOptions) error {
	log := p.logger.At("Initialize")

	// if err := p.initializeAtom(); err != nil {
	// 	return log.Error(err)
	// }

	dc, err := NewDeploymentController(p)
	if err != nil {
		return log.Error(err)
	}

	ec, err := NewEventController(p)
	if err != nil {
		return log.Error(err)
	}

	nc, err := NewNodeController(p)
	if err != nil {
		return log.Error(err)
	}

	pc, err := NewPodController(p)
	if err != nil {
		return log.Error(err)
	}

	go dc.Run()
	go ec.Run()
	go nc.Run()
	go pc.Run()

	return log.Success()
}

func (p *Provider) Context() context.Context {
	return p.ctx
}

func (p *Provider) WithContext(ctx context.Context) structs.Provider {
	pp := *p
	pp.ctx = ctx
	return &pp
}

// func (p *Provider) initializeAtom() error {
// 	params := map[string]interface{}{
// 		"Version": p.Version,
// 	}

// 	data, err := p.RenderTemplate("atom", params)
// 	if err != nil {
// 		return err
// 	}

// 	cmd := exec.Command("kubectl", "apply", "-f", "-")

// 	cmd.Stdin = bytes.NewReader(data)

// 	if out, err := cmd.CombinedOutput(); err != nil {
// 		return errors.New(strings.TrimSpace(string(out)))
// 	}

// 	return nil
// }
