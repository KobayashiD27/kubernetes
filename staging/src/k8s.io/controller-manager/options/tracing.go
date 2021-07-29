/*
Copyright 2021 The Kubernetes Authors.
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

package options

import (
	"context"

	"github.com/spf13/pflag"
	"go.opentelemetry.io/otel/exporters/otlp/otlpgrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/semconv"

	"go.opentelemetry.io/otel/trace"
	"k8s.io/component-base/traces"
	cmconfig "k8s.io/controller-manager/config"
)

const controllerMangerService = "kube-controller-manager"

var tp trace.TracerProvider

// TracingOptions contain configuration options for tracing
// exporters
type TracingOptions struct {
}

// NewTracingOptions creates a new instance of TracingOptions
func NewTracingOptions() *TracingOptions {
	return &TracingOptions{}
}

// AddFlags adds flags related to tracing to the specified FlagSet
func (o *TracingOptions) AddFlags(fs *pflag.FlagSet) {
	return
}

// ApplyTo fills up Tracing config with options.
func (o *TracingOptions) ApplyTo(cfg *cmconfig.GenericControllerManagerConfiguration) error {
	opts := []otlpgrpc.Option{}
	sampler := sdktrace.NeverSample()
	resourceOpts := []resource.Option{
		resource.WithAttributes(
			semconv.ServiceNameKey.String(controllerMangerService),
		),
	}
	tp = traces.NewProvider(context.Background(), sampler, resourceOpts, opts...)
	return nil
}

// Validate verifies flags passed to TracingOptions.
func (o *TracingOptions) Validate() (errs []error) {
	return
}
