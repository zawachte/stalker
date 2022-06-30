package cadvisor

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"path"
	"time"

	// Register supported container handlers.
	_ "github.com/google/cadvisor/container/containerd/install"
	_ "github.com/google/cadvisor/container/crio/install"
	_ "github.com/google/cadvisor/container/systemd/install"

	"github.com/google/cadvisor/cache/memory"
	cadvisormetrics "github.com/google/cadvisor/container"
	"github.com/google/cadvisor/events"
	cadvisorapi "github.com/google/cadvisor/info/v1"
	cadvisorapiv2 "github.com/google/cadvisor/info/v2"
	"github.com/google/cadvisor/manager"
	"github.com/google/cadvisor/utils/sysfs"
	"k8s.io/utils/pointer"
)

type cadvisorClient struct {
	imageFsInfoProvider ImageFsInfoProvider
	rootPath            string
	man                 manager.Manager
}

const statsCacheDuration = 2 * time.Minute
const maxHousekeepingInterval = 15 * time.Second
const defaultHousekeepingInterval = 10 * time.Second
const allowDynamicHousekeeping = true

func init() {
	// Override cAdvisor flag defaults.
	flagOverrides := map[string]string{
		// Override the default cAdvisor housekeeping interval.
		"housekeeping_interval": defaultHousekeepingInterval.String(),
		// Disable event storage by default.
		"event_storage_event_limit": "default=0",
		"event_storage_age_limit":   "default=0",
	}
	for name, defaultValue := range flagOverrides {
		if f := flag.Lookup(name); f != nil {
			f.DefValue = defaultValue
			f.Value.Set(defaultValue)
		} else {
			//klog.ErrorS(nil, "Expected cAdvisor flag not found", "flag", name)
		}
	}
}

// ImageFsInfoProvider informs cAdvisor how to find imagefs for container images.
type ImageFsInfoProvider interface {
	// ImageFsInfoLabel returns the label cAdvisor should use to find the filesystem holding container images.
	ImageFsInfoLabel() (string, error)
}

// Interface is an abstract interface for testability.  It abstracts the interface to cAdvisor.
type Interface interface {
	Start() error
	//	DockerContainer(name string, req *cadvisorapi.ContainerInfoRequest) (cadvisorapi.ContainerInfo, error)
	ContainerInfo(name string, req *cadvisorapi.ContainerInfoRequest) (*cadvisorapi.ContainerInfo, error)
	ContainerInfoV2(name string, options cadvisorapiv2.RequestOptions) (map[string]cadvisorapiv2.ContainerInfo, error)
	//	GetRequestedContainersInfo(containerName string, options cadvisorapiv2.RequestOptions) (map[string]*cadvisorapi.ContainerInfo, error)
	SubcontainerInfo(name string, req *cadvisorapi.ContainerInfoRequest) (map[string]*cadvisorapi.ContainerInfo, error)
	MachineInfo() (*cadvisorapi.MachineInfo, error)

	VersionInfo() (*cadvisorapi.VersionInfo, error)

	// Returns usage information about the filesystem holding container images.
	ImagesFsInfo() ([]cadvisorapiv2.FsInfo, error)

	// Returns usage information about the root filesystem.
	RootFsInfo() (cadvisorapiv2.FsInfo, error)

	// Get events streamed through passedChannel that fit the request.
	WatchEvents(request *events.Request) (*events.EventChannel, error)

	// Get filesystem information for the filesystem that contains the given file.
	//	GetDirFsInfo(path string) (cadvisorapiv2.FsInfo, error)
}

// New creates a new cAdvisor Interface for linux systems.
func New(imageFsInfoProvider ImageFsInfoProvider, rootPath string, cgroupRoots []string, usingLegacyStats bool) (Interface, error) {
	sysFs := sysfs.NewRealSysFs()

	includedMetrics := cadvisormetrics.MetricSet{
		cadvisormetrics.CpuUsageMetrics:     struct{}{},
		cadvisormetrics.MemoryUsageMetrics:  struct{}{},
		cadvisormetrics.CpuLoadMetrics:      struct{}{},
		cadvisormetrics.DiskIOMetrics:       struct{}{},
		cadvisormetrics.NetworkUsageMetrics: struct{}{},
		cadvisormetrics.AppMetrics:          struct{}{},
		cadvisormetrics.ProcessMetrics:      struct{}{},
	}

	// includedMetrics[cadvisormetrics.DiskUsageMetrics] = struct{}{}

	duration := maxHousekeepingInterval
	housekeepingConfig := manager.HouskeepingConfig{
		Interval:     &duration,
		AllowDynamic: pointer.BoolPtr(allowDynamicHousekeeping),
	}

	// Create the cAdvisor container manager.
	m, err := manager.New(memory.New(statsCacheDuration, nil), sysFs, housekeepingConfig, includedMetrics, http.DefaultClient, cgroupRoots, []string{}, "", time.Second)
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(rootPath); err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(path.Clean(rootPath), 0750); err != nil {
				return nil, fmt.Errorf("error creating root directory %q: %v", rootPath, err)
			}
		} else {
			return nil, fmt.Errorf("failed to Stat %q: %v", rootPath, err)
		}
	}

	return &cadvisorClient{
		imageFsInfoProvider: imageFsInfoProvider,
		rootPath:            rootPath,
		man:                 m,
	}, nil
}

func (cc *cadvisorClient) Start() error {
	return cc.man.Start()
}

func (cc *cadvisorClient) ContainerInfo(name string, req *cadvisorapi.ContainerInfoRequest) (*cadvisorapi.ContainerInfo, error) {
	return cc.man.GetContainerInfo(name, req)
}

func (cc *cadvisorClient) ContainerInfoV2(name string, options cadvisorapiv2.RequestOptions) (map[string]cadvisorapiv2.ContainerInfo, error) {
	return cc.man.GetContainerInfoV2(name, options)
}

func (cc *cadvisorClient) VersionInfo() (*cadvisorapi.VersionInfo, error) {
	return cc.man.GetVersionInfo()
}

func (cc *cadvisorClient) SubcontainerInfo(name string, req *cadvisorapi.ContainerInfoRequest) (map[string]*cadvisorapi.ContainerInfo, error) {
	infos, err := cc.man.SubcontainersInfo(name, req)
	if err != nil && len(infos) == 0 {
		return nil, err
	}

	result := make(map[string]*cadvisorapi.ContainerInfo, len(infos))
	for _, info := range infos {
		result[info.Name] = info
	}
	return result, err
}

func (cc *cadvisorClient) MachineInfo() (*cadvisorapi.MachineInfo, error) {
	return cc.man.GetMachineInfo()
}

func (cc *cadvisorClient) ImagesFsInfo() ([]cadvisorapiv2.FsInfo, error) {
	label, err := cc.imageFsInfoProvider.ImageFsInfoLabel()
	if err != nil {
		return nil, err
	}
	return cc.man.GetFsInfo(label)
}

func (cc *cadvisorClient) RootFsInfo() (cadvisorapiv2.FsInfo, error) {
	return cc.man.GetDirFsInfo(cc.rootPath)
}

func (cc *cadvisorClient) getFsInfo(label string) (cadvisorapiv2.FsInfo, error) {
	res, err := cc.man.GetFsInfo(label)
	if err != nil {
		return cadvisorapiv2.FsInfo{}, err
	}
	if len(res) == 0 {
		return cadvisorapiv2.FsInfo{}, fmt.Errorf("failed to find information for the filesystem labeled %q", label)
	}
	// TODO(vmarmol): Handle this better when a label has more than one image filesystem.
	if len(res) > 1 {
		//	klog.InfoS("More than one filesystem labeled. Only using the first one", "label", label, "fileSystem", res)
	}

	return res[0], nil
}

func (cc *cadvisorClient) WatchEvents(request *events.Request) (*events.EventChannel, error) {
	return cc.man.WatchForEvents(request)
}

// imageFsInfoProvider knows how to translate the configured runtime
// to its file system label for images.
type imageFsInfoProvider struct {
	runtimeEndpoint string
}

// ImageFsInfoLabel returns the image fs label for the configured runtime.
// For remote runtimes, it handles additional runtimes natively understood by cAdvisor.
func (i *imageFsInfoProvider) ImageFsInfoLabel() (string, error) {
	return "", fmt.Errorf("no imagefs label for configured runtime")
}

// NewImageFsInfoProvider returns a provider for the specified runtime configuration.
func NewImageFsInfoProvider(runtimeEndpoint string) ImageFsInfoProvider {
	return &imageFsInfoProvider{runtimeEndpoint: runtimeEndpoint}
}
