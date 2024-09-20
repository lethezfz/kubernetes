/*
Copyright 2022 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package plugin

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	utilversion "k8s.io/apimachinery/pkg/util/version"
	"k8s.io/klog/v2"
	drapbv1alpha4 "k8s.io/kubelet/pkg/apis/dra/v1alpha4"
	drapbv1beta1 "k8s.io/kubelet/pkg/apis/dra/v1beta1"
	"k8s.io/kubernetes/pkg/kubelet/metrics"
)

// NewDRAPluginClient returns a wrapper around those gRPC methods of a DRA
// driver kubelet plugin which need to be called by kubelet. The wrapper
// handles gRPC connection management and logging. Connections are reused
// across different NewDRAPluginClient calls.
func NewDRAPluginClient(pluginName string) (*Plugin, error) {
	if pluginName == "" {
		return nil, fmt.Errorf("plugin name is empty")
	}

	existingPlugin := draPlugins.get(pluginName)
	if existingPlugin == nil {
		return nil, fmt.Errorf("plugin name %s not found in the list of registered DRA plugins", pluginName)
	}

	return existingPlugin, nil
}

type Plugin struct {
	name          string
	backgroundCtx context.Context
	cancel        func(cause error)

	mutex                   sync.Mutex
	conn                    *grpc.ClientConn
	supportedAPI            apiVersion
	endpoint                string
	highestSupportedVersion *utilversion.Version
	clientCallTimeout       time.Duration
}

type apiVersion string

const (
	apiV1alpha4 = apiVersion("v1alpha4")
	apiV1beta1  = apiVersion("v1beta1")
)

func (p *Plugin) getOrCreateGRPCConn() (*grpc.ClientConn, apiVersion, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.conn != nil {
		return p.conn, p.supportedAPI, nil
	}

	ctx := p.backgroundCtx
	logger := klog.FromContext(ctx)

	network := "unix"
	logger.V(4).Info("Creating new gRPC connection", "protocol", network, "endpoint", p.endpoint)
	// grpc.Dial is deprecated. grpc.NewClient should be used instead.
	// For now this gets ignored because this function is meant to establish
	// the connection, with the one second timeout below. Perhaps that
	// approach should be reconsidered?
	//nolint:staticcheck
	conn, err := grpc.Dial(
		p.endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, target string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, target)
		}),
		grpc.WithChainUnaryInterceptor(newMetricsInterceptor(p.name)),
	)
	if err != nil {
		return nil, "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if ok := conn.WaitForStateChange(ctx, connectivity.Connecting); !ok {
		return nil, "", errors.New("timed out waiting for gRPC connection to be ready")
	}

	p.conn = conn
	return p.conn, "", nil
}

func (p *Plugin) NodePrepareResources(
	ctx context.Context,
	req *drapbv1beta1.NodePrepareResourcesRequest,
	opts ...grpc.CallOption,
) (*drapbv1beta1.NodePrepareResourcesResponse, error) {
	logger := klog.FromContext(ctx)
	logger.V(4).Info("Calling NodePrepareResources rpc", "request", req)

	conn, supportedAPI, err := p.getOrCreateGRPCConn()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, p.clientCallTimeout)
	defer cancel()

	var response *drapbv1beta1.NodePrepareResourcesResponse
	switch supportedAPI {
	case apiV1beta1:
		nodeClient := drapbv1beta1.NewNodeClient(conn)
		response, err = nodeClient.NodePrepareResources(ctx, req)
	case apiV1alpha4:
		nodeClient := drapbv1alpha4.NewNodeClient(conn)
		response, err = nodeClient.NodePrepareResources(ctx, req)
	default:
		// Try it, fall back if necessary.
		supportedAPI = apiV1beta1
		nodeClient := drapbv1beta1.NewNodeClient(conn)
		response, err = nodeClient.NodePrepareResources(ctx, req)
		if err != nil && status.Convert(err).Code() == codes.Unimplemented {
			supportedAPI = apiV1alpha4
			nodeClient := drapbv1alpha4.NewNodeClient(conn)
			response, err = nodeClient.NodePrepareResources(ctx, req)
		}
		if err == nil || status.Convert(err).Code() != codes.Unimplemented {
			// Store discovered version for future use.
			p.mutex.Lock()
			p.supportedAPI = supportedAPI
			p.mutex.Unlock()
		}
	}
	logger.V(4).Info("Done calling NodePrepareResources rpc", "response", response, "err", err)
	return response, err
}

func (p *Plugin) NodeUnprepareResources(
	ctx context.Context,
	req *drapbv1beta1.NodeUnprepareResourcesRequest,
	opts ...grpc.CallOption,
) (*drapbv1beta1.NodeUnprepareResourcesResponse, error) {
	logger := klog.FromContext(ctx)
	logger.V(4).Info("Calling NodeUnprepareResource rpc", "request", req)

	conn, supportedAPI, err := p.getOrCreateGRPCConn()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, p.clientCallTimeout)
	defer cancel()

	var response *drapbv1beta1.NodeUnprepareResourcesResponse
	switch supportedAPI {
	case apiV1beta1:
		nodeClient := drapbv1beta1.NewNodeClient(conn)
		response, err = nodeClient.NodeUnprepareResources(ctx, req)
	case apiV1alpha4:
		nodeClient := drapbv1alpha4.NewNodeClient(conn)
		response, err = nodeClient.NodeUnprepareResources(ctx, req)
	default:
		// Try it, fall back if necessary.
		supportedAPI = apiV1beta1
		nodeClient := drapbv1beta1.NewNodeClient(conn)
		response, err = nodeClient.NodeUnprepareResources(ctx, req)
		if err != nil && status.Convert(err).Code() == codes.Unimplemented {
			supportedAPI = apiV1alpha4
			nodeClient := drapbv1alpha4.NewNodeClient(conn)
			response, err = nodeClient.NodeUnprepareResources(ctx, req)
		}
		if err == nil || status.Convert(err).Code() != codes.Unimplemented {
			// Store discovered version for future use.
			p.mutex.Lock()
			p.supportedAPI = supportedAPI
			p.mutex.Unlock()
		}
	}
	logger.V(4).Info("Done calling NodeUnprepareResources rpc", "response", response, "err", err)
	return response, err
}

func newMetricsInterceptor(pluginName string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, conn *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		start := time.Now()
		err := invoker(ctx, method, req, reply, conn, opts...)
		metrics.DRAGRPCOperationsDuration.WithLabelValues(pluginName, method, status.Code(err).String()).Observe(time.Since(start).Seconds())
		return err
	}
}
