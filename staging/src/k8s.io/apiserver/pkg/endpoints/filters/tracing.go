package filters

import (
	"net/http"
	"k8s.io/klog"
)

type contextKey struct{}

// WithTracing adds tracing to log if the incoming request is sampled
func WithTracing(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request){
		hd := req.Header
		if(hd.Get("X-B3-Sampled") == "1"){
			klog.V(3).Infof("http tracing demo Sampled:%s, TraceID:%s, SpanID:%s", hd.Get("X-B3-Sampled"), hd.Get("X-B3-TraceID"), hd.Get("X-B3-SpanID"))
		}
		handler.ServeHTTP(w, req)
	})
} 

