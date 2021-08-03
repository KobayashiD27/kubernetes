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

package traces

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/exporters/otlp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpgrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/client-go/transport"
	"k8s.io/klog/v2"
)

// NewProvider initializes tracing in the component, and enforces recommended tracing behavior.
func NewProvider(ctx context.Context, baseSampler sdktrace.Sampler, resourceOpts []resource.Option, opts ...otlpgrpc.Option) trace.TracerProvider {
	opts = append(opts, otlpgrpc.WithInsecure())
	driver := otlpgrpc.NewDriver(opts...)
	exporter, err := otlp.NewExporter(ctx, driver)
	if err != nil {
		klog.Fatalf("Failed to create OTLP exporter: %v", err)
	}

	res, err := resource.New(ctx, resourceOpts...)
	if err != nil {
		klog.Fatalf("Failed to create resource: %v", err)
	}

	bsp := sdktrace.NewBatchSpanProcessor(exporter)

	return sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.ParentBased(baseSampler)),
		sdktrace.WithSpanProcessor(bsp),
		sdktrace.WithResource(res),
	)
}

// WrapperFor can be used to add tracing to a *rest.Config. Example usage:
//     tp := traces.NewProvider(...)
//     config, _ := rest.InClusterConfig()
//     config.Wrap(traces.WrapperFor(&tp))
//     kubeclient, _ := clientset.NewForConfig(config)
func WrapperFor(tp *trace.TracerProvider) transport.WrapperFunc {
	return func(rt http.RoundTripper) http.RoundTripper {
		opts := []otelhttp.Option{
			otelhttp.WithPropagators(Propagators()),
		}
		if tp != nil {
			opts = append(opts, otelhttp.WithTracerProvider(*tp))
		}
		// Even if there is no TracerProvider, the otelhttp still handles context propagation.
		// See https://github.com/open-telemetry/opentelemetry-go/tree/main/example/passthrough
		return otelhttp.NewTransport(rt, opts...)
	}
}

// Propagators returns the recommended set of propagators.
func Propagators() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
}

type traceContextKey string

const (
	traceContextsKey traceContextKey = "traceContexts"
)

func ManagedFieldsToContext(ctx context.Context, managedFields []metav1.ManagedFieldsEntry, generation int64) context.Context {
	if len(managedFields) == 0 {
		return ctx
	}

	// Inject baggage
	traceContextsMap := make(map[string]struct{})
	maxIndex := 0
	for index, managedField := range managedFields {
		var traceGeneration int64 = 0
		if managedField.TraceGeneration != nil {
			traceGeneration = *managedField.TraceGeneration
		}
		if traceGeneration > generation {
			for _, traceContext := range managedField.TraceContexts {
				traceContextsMap[traceContext] = struct{}{}
			}
		}
		if index > 0 && managedField.Time != nil && managedFields[maxIndex].Time != nil && managedField.Time.After(managedFields[maxIndex].Time.Time) {
			maxIndex = index
		}
	}

	if len(traceContextsMap) == 0 {
		for _, traceContext := range managedFields[maxIndex].TraceContexts {
			traceContextsMap[traceContext] = struct{}{}
		}
	}

	traceContexts := []string{}
	for traceContext := range traceContextsMap {
		if traceContext == "" {
			continue
		}
		traceContexts = append(traceContexts, traceContext)
	}

	value, _ := json.Marshal(traceContexts)
	ctx = baggage.ContextWithValues(ctx, attribute.String(string(traceContextsKey), string(value)))

	// Inject span context
	if len(traceContexts) == 0 {
		return ctx
	}

	value, _ = hex.DecodeString(traceContexts[0])
	var traceID trace.TraceID
	copy(traceID[:], value[:])
	spanContext := trace.SpanContext{}
	spanContext = spanContext.WithTraceID(traceID)
	spanContext = spanContext.WithSpanID(trace.SpanID{1})
	return trace.ContextWithSpanContext(ctx, spanContext)
}

func WithTraceContext(ctx context.Context) context.Context {
	// Baggage trace context
	value := baggage.Value(ctx, attribute.Key(traceContextsKey)).AsString()
	traceContexts := make([]string, 0)
	json.Unmarshal([]byte(value), &traceContexts)

	// New trace context
	if len(traceContexts) == 0 {
		traceContexts = append(traceContexts, trace.SpanFromContext(ctx).SpanContext().TraceID().String())
	}

	return context.WithValue(ctx, traceContextsKey, traceContexts)
}

func ValueTraceContext(ctx context.Context) []string {
	traceContexts, ok := ctx.Value(traceContextsKey).([]string)
	if !ok {
		return nil
	}

	return traceContexts
}
