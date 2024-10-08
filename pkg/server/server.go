package server

import (
	"context"
	"crypto/md5"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	deviceapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

const (
	resourceName = "micro.plugin"
	microPath    = "/etc/micro"
	microSocket  = "micro.sock"
	kubeSocket   = "kubelet.sock"
	pluginPath   = "/var/lib/kubelet/device-plugins"
)

const (
	maxRestartNum  = 5
	maxCrashPeriod = 3600
)

// MicroDeviceServer is a device plugin server
type MicroDeviceServer struct {
	devices   map[string]*deviceapi.Device
	serv      *grpc.Server
	ctx       context.Context
	cancel    context.CancelFunc
	notify    chan bool
	restarted bool
}

// NewMicroDeviceServer creates a new device plugin server
func NewMicroDeviceServer() *MicroDeviceServer {
	ctx, cancel := context.WithCancel(context.Background())
	return &MicroDeviceServer{
		devices:   make(map[string]*deviceapi.Device),
		serv:      grpc.NewServer(grpc.EmptyServerOption{}),
		ctx:       ctx,
		cancel:    cancel,
		notify:    make(chan bool),
		restarted: false,
	}
}

// Run starts the micro device plugin server
func (s *MicroDeviceServer) Run() error {
	if err := s.findDevice(); err != nil {
		slog.Error("find device failed", "err", err)
		return err
	}

	go func() {
		err := s.watchDevice()
		if err != nil {
			slog.Error("watch device", "err", err)
		}
	}()

	deviceapi.RegisterDevicePluginServer(s.serv, s)
	err := syscall.Unlink(pluginPath + microSocket)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	listener, err := net.Listen("unix", pluginPath+microSocket)
	if err != nil {
		return err
	}

	go func() {
		startTime := time.Now()
		restartNum := 0
		for {
			slog.Info("starting RPC server", "resource", resourceName)
			err = s.serv.Serve(listener)
			if err == nil {
				break
			}

			slog.Info("RPC server crashed", "resource", resourceName, "err", err)

			if restartNum > maxRestartNum {
				slog.Error("micro device plugin has repeatedly crashed recently. Quitting")
			}

			crashSeconds := time.Since(startTime).Seconds()
			if crashSeconds > maxCrashPeriod {
				restartNum = 1
			} else {
				restartNum++
			}
		}
	}()

	conn, err := s.dial(microSocket, time.Second*5)
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}

// RegisterToKubelet registers the micro device plugin with kubelet
func (s *MicroDeviceServer) RegisterToKubelet() error {
	sockFile := filepath.Join(pluginPath + kubeSocket)
	conn, err := s.dial(sockFile, time.Second*5)
	if err != nil {
		return err
	}
	defer conn.Close()

	client := deviceapi.NewRegistrationClient(conn)
	req := &deviceapi.RegisterRequest{
		Version:      deviceapi.Version,
		Endpoint:     path.Base(pluginPath + microSocket),
		ResourceName: resourceName,
	}
	slog.Info("Register plugin to kubelet", "endpoint", req.Endpoint)
	_, err = client.Register(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

// Allocate make the device avilable in container
func (s *MicroDeviceServer) Allocate(ctx context.Context, reqs *deviceapi.AllocateRequest) (*deviceapi.AllocateResponse, error) {
	result := &deviceapi.AllocateResponse{}
	for _, req := range reqs.ContainerRequests {
		slog.Info("received request", "data", req)
		resp := deviceapi.ContainerAllocateResponse{
			Envs: map[string]string{
				"MICRO_DEVICES": strings.Join(req.DevicesIDs, ","),
			},
		}
		result.ContainerResponses = append(result.ContainerResponses, &resp)
	}
	return result, nil
}

// ListAndWatch return a stream of list devices and update that stream whenever changes
func (s *MicroDeviceServer) ListAndWatch(e *deviceapi.Empty, srv deviceapi.DevicePlugin_ListAndWatchServer) error {
	slog.Info("ListAndWatch started")
	devs := make([]*deviceapi.Device, len(s.devices))
	i := 0
	for _, dev := range s.devices {
		devs[i] = dev
		i++
	}
	err := srv.Send(&deviceapi.ListAndWatchResponse{Devices: devs})
	if err != nil {
		slog.Error("ListAndWatch send device failed", "error", err)
		return err
	}

	for {
		slog.Info("waiting for device change ...")
		select {
		case <-s.notify:
			slog.Info("device change detected", "num", len(s.devices))
			devs := make([]*deviceapi.Device, len(s.devices))
			i := 0
			for _, dev := range s.devices {
				devs[i] = dev
				i++
			}
			srv.Send(&deviceapi.ListAndWatchResponse{Devices: devs})
		case <-s.ctx.Done():
			slog.Info("ListAndWatch exited")
			return nil
		}
	}
}

// GetDevicePluginOptions return options for the device plugin
func (s *MicroDeviceServer) GetDevicePluginOptions(context.Context, *deviceapi.Empty) (*deviceapi.DevicePluginOptions, error) {
	return &deviceapi.DevicePluginOptions{PreStartRequired: true}, nil
}

// GetPreferredAllocation return the devices chosen for allocation based on the given options
func (s *MicroDeviceServer) GetPreferredAllocation(context.Context, *deviceapi.PreferredAllocationRequest) (*deviceapi.PreferredAllocationResponse, error) {
	slog.Info("GetPreferredAllocation executed")
	return nil, nil
}

// PreStartContainer is called during the device plugin pod starting
func (s *MicroDeviceServer) PreStartContainer(context.Context, *deviceapi.PreStartContainerRequest) (*deviceapi.PreStartContainerResponse, error) {
	slog.Info("PreStartContainer executed")
	return &deviceapi.PreStartContainerResponse{}, nil
}

// findDevice discovers the micro devices on machine
func (s *MicroDeviceServer) findDevice() error {
	dir, err := os.ReadDir(microPath)
	if err != nil {
		slog.Error("failed to read micro path", "err", err)
		return err
	}
	for _, f := range dir {
		if f.IsDir() {
			continue
		}
		byteID := md5.Sum([]byte(f.Name()))
		id := string(byteID[:])
		s.devices[f.Name()] = &deviceapi.Device{
			ID:     id,
			Health: deviceapi.Healthy,
		}
		slog.Info("find device", "name", f.Name(), "ID", id)
	}
	return nil
}

func (s *MicroDeviceServer) watchDevice() error {
	slog.Info("watching micro devices ...")
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("fsnotify NewWatcher error: %w", err)
	}
	defer w.Close()

	done := make(chan bool)
	go func() {
		defer func() {
			done <- true
			slog.Info("watch device exit")
		}()

		for {
			select {
			case event, ok := <-w.Events:
				if !ok {
					continue
				}
				slog.Info("device event", "kind", event.Op.String())

				if event.Op&fsnotify.Create == fsnotify.Create {
					byteID := md5.Sum([]byte(event.Name))
					id := string(byteID[:])
					s.devices[event.Name] = &deviceapi.Device{
						ID:     id,
						Health: deviceapi.Healthy,
					}
					slog.Info("found new micro device ", "name", event.Name, "id", id)
				}

				if event.Op&fsnotify.Remove == fsnotify.Remove {
					delete(s.devices, event.Name)
					s.notify <- true
					slog.Info("device deleted", "name", event.Name)
				}

			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				slog.Error("watcher", "err", err)

			case <-s.ctx.Done():
				break
			}
		}
	}()

	if err := w.Add(microPath); err != nil {
		return fmt.Errorf("watch device error: %w", err)
	}
	<-done

	return nil
}

func (s *MicroDeviceServer) dial(unixSocketPath string, timeout time.Duration) (*grpc.ClientConn, error) {
	return grpc.NewClient(unixSocketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return net.DialTimeout("unix", addr, timeout)
		}),
	)
}
