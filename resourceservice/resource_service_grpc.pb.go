// Code generated by protoc-gen-go-grpc. DO NOT EDIT.
// versions:
// - protoc-gen-go-grpc v1.5.1
// - protoc             v3.21.12
// source: resourceservice/resource_service.proto

package maschinist

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
// Requires gRPC-Go v1.64.0 or later.
const _ = grpc.SupportPackageIsVersion9

const (
	ResourceService_GetResources_FullMethodName = "/resourceservice.ResourceService/GetResources"
)

// ResourceServiceClient is the client API for ResourceService service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
//
// Service definition
type ResourceServiceClient interface {
	GetResources(ctx context.Context, in *ResourceRequest, opts ...grpc.CallOption) (*ResourceListResponse, error)
}

type resourceServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewResourceServiceClient(cc grpc.ClientConnInterface) ResourceServiceClient {
	return &resourceServiceClient{cc}
}

func (c *resourceServiceClient) GetResources(ctx context.Context, in *ResourceRequest, opts ...grpc.CallOption) (*ResourceListResponse, error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	out := new(ResourceListResponse)
	err := c.cc.Invoke(ctx, ResourceService_GetResources_FullMethodName, in, out, cOpts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ResourceServiceServer is the server API for ResourceService service.
// All implementations must embed UnimplementedResourceServiceServer
// for forward compatibility.
//
// Service definition
type ResourceServiceServer interface {
	GetResources(context.Context, *ResourceRequest) (*ResourceListResponse, error)
	mustEmbedUnimplementedResourceServiceServer()
}

// UnimplementedResourceServiceServer must be embedded to have
// forward compatible implementations.
//
// NOTE: this should be embedded by value instead of pointer to avoid a nil
// pointer dereference when methods are called.
type UnimplementedResourceServiceServer struct{}

func (UnimplementedResourceServiceServer) GetResources(context.Context, *ResourceRequest) (*ResourceListResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetResources not implemented")
}
func (UnimplementedResourceServiceServer) mustEmbedUnimplementedResourceServiceServer() {}
func (UnimplementedResourceServiceServer) testEmbeddedByValue()                         {}

// UnsafeResourceServiceServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to ResourceServiceServer will
// result in compilation errors.
type UnsafeResourceServiceServer interface {
	mustEmbedUnimplementedResourceServiceServer()
}

func RegisterResourceServiceServer(s grpc.ServiceRegistrar, srv ResourceServiceServer) {
	// If the following call pancis, it indicates UnimplementedResourceServiceServer was
	// embedded by pointer and is nil.  This will cause panics if an
	// unimplemented method is ever invoked, so we test this at initialization
	// time to prevent it from happening at runtime later due to I/O.
	if t, ok := srv.(interface{ testEmbeddedByValue() }); ok {
		t.testEmbeddedByValue()
	}
	s.RegisterService(&ResourceService_ServiceDesc, srv)
}

func _ResourceService_GetResources_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ResourceRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ResourceServiceServer).GetResources(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: ResourceService_GetResources_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ResourceServiceServer).GetResources(ctx, req.(*ResourceRequest))
	}
	return interceptor(ctx, in, info, handler)
}

// ResourceService_ServiceDesc is the grpc.ServiceDesc for ResourceService service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var ResourceService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "resourceservice.ResourceService",
	HandlerType: (*ResourceServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "GetResources",
			Handler:    _ResourceService_GetResources_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "resourceservice/resource_service.proto",
}
