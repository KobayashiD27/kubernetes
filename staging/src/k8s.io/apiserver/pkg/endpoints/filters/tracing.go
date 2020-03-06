package filters





import (
	"net/http"

	"k8s.io/klog"

	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/trace"
)

// WithTracing adds tracing to requests if the incoming request is sampled
func WithTracing(handler http.Handler) http.Handler {
	handler = &ochttp.Handler{
		Handler: handler,
		StartOptions: trace.StartOptions{Sampler: trace.ProbabilitySampler(0)},
	}
	return http.HandleFunc(func(w http.ResponseWriter, req *http.Request){
		klog.V(3).Infof("otel test req:%s", req)
		handler.ServeHTTP(w, req)
	})

}
