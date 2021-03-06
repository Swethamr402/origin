package web_console

import (
	"fmt"
	"io/ioutil"
	"path"

	yaml "gopkg.in/yaml.v2"

	"github.com/golang/glog"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	kclientcmd "k8s.io/client-go/tools/clientcmd"

	"github.com/openshift/origin/pkg/cmd/util/variable"
	"github.com/openshift/origin/pkg/oc/bootstrap"
	"github.com/openshift/origin/pkg/oc/bootstrap/clusterup/componentinstall"
	"github.com/openshift/origin/pkg/oc/bootstrap/docker/dockerhelper"
	"github.com/openshift/origin/pkg/oc/errors"
)

const (
	consoleNamespace = "openshift-web-console"
)

type WebConsoleComponentOptions struct {
	OCImage          string
	MasterConfigDir  string
	ImageFormat      string
	PublicMasterURL  string
	PublicConsoleURL string
	PublicLoggingURL string
	PublicMetricsURL string
	ServerLogLevel   int
}

func (c *WebConsoleComponentOptions) Name() string {
	return "openshift-web-console"
}

func (c *WebConsoleComponentOptions) Install(dockerClient dockerhelper.Interface, logdir string) error {
	clusterAdminKubeConfigBytes, err := ioutil.ReadFile(path.Join(c.MasterConfigDir, "admin.kubeconfig"))
	if err != nil {
		return err
	}
	restConfig, err := kclientcmd.RESTConfigFromKubeConfig(clusterAdminKubeConfigBytes)
	if err != nil {
		return errors.NewError("cannot obtain API clients").WithCause(err)
	}
	kubeClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return errors.NewError("cannot obtain API clients").WithCause(err)
	}

	// parse the YAML to edit
	var consoleConfig map[string]interface{}
	if err := yaml.Unmarshal(bootstrap.MustAsset("install/origin-web-console/console-config.yaml"), &consoleConfig); err != nil {
		return errors.NewError("cannot parse web console config as YAML").WithCause(err)
	}

	// update config values
	clusterInfo, ok := consoleConfig["clusterInfo"].(map[interface{}]interface{})
	if !ok {
		return errors.NewError("cannot read clusterInfo in web console config")
	}

	clusterInfo["consolePublicURL"] = c.PublicConsoleURL + "/"
	clusterInfo["masterPublicURL"] = c.PublicMasterURL
	if len(c.PublicLoggingURL) > 0 {
		clusterInfo["loggingPublicURL"] = c.PublicLoggingURL
	}
	if len(c.PublicMetricsURL) > 0 {
		clusterInfo["metricsPublicURL"] = c.PublicMetricsURL
	}

	// serialize it back out as a string to use as a template parameter
	updatedConfig, err := yaml.Marshal(consoleConfig)
	if err != nil {
		return errors.NewError("cannot serialize web console config").WithCause(err)
	}

	imageTemplate := variable.NewDefaultImageTemplate()
	imageTemplate.Format = c.ImageFormat
	imageTemplate.Latest = false

	params := map[string]string{
		"API_SERVER_CONFIG": string(updatedConfig),
		"IMAGE":             imageTemplate.ExpandOrDie("web-console"),
		"LOGLEVEL":          fmt.Sprintf("%d", c.ServerLogLevel),
		"NAMESPACE":         consoleNamespace,
	}

	component := componentinstall.Template{
		Name:            "webconsole",
		Namespace:       consoleNamespace,
		InstallTemplate: bootstrap.MustAsset("install/origin-web-console/console-template.yaml"),

		// wait until the webconsole is ready
		WaitCondition: func() (bool, error) {
			glog.V(2).Infof("polling for web console server availability")
			ds, err := kubeClient.AppsV1().Deployments(consoleNamespace).Get("webconsole", metav1.GetOptions{})
			if err != nil {
				return false, err
			}
			if ds.Status.ReadyReplicas > 0 {
				return true, nil
			}
			return false, nil
		},
	}

	// instantiate the web console template
	return component.MakeReady(
		c.OCImage,
		clusterAdminKubeConfigBytes,
		params).Install(dockerClient, logdir)
}
