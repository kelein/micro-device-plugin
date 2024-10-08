package server

import (
	"context"

	"google.golang.org/grpc"
)

// MicroDeviceServer is a device plugin server
type MicroDeviceServer struct {
	// devices map[string]*pluginapi.Device
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
		serv:      grpc.NewServer(grpc.EmptyServerOption{}),
		ctx:       ctx,
		cancel:    cancel,
		notify:    make(chan bool),
		restarted: false,
	}
}

// Run starts the micro device plugin server
func (s *MicroDeviceServer) Run() error { return nil }

// RegisterToKubelet registers the micro device plugin with kubelet
func (s *MicroDeviceServer) RegisterToKubelet() error { return nil }
