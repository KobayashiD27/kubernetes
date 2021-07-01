/*
Copyright 2020 The Kubernetes Authors.

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

package httptrace

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"k8s.io/klog/v2"

	apitrace "go.opentelemetry.io/otel/api/trace"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"go.opentelemetry.io/otel/api/global"
)

type contextKeyType int

const spanContextAnnotationKey string = "trace.kubernetes.io/context"
const contextProcessKey string = "contextProcessKey"

func stringToSpanContext(sc string) apitrace.SpanContext {
	id, _ := apitrace.IDFromHex(sc[0:32])
	spanid, _ := apitrace.SpanIDFromHex(sc[33:49])
	return apitrace.SpanContext{
		TraceID: id,
		SpanID:  spanid,
	}
}

/*
   fieldsV1:
     f:status:
       f:conditions:
         k:{"type":"ContainersReady"}:
           f:lastTransitionTime: {}
           f:status: {}
         k:{"type":"Ready"}:
           f:lastTransitionTime: {}
           f:status: {}
       f:containerStatuses: {}
       f:phase: {}
       f:podIP: {}
       f:podIPs:
         .: {}
         k:{"ip":"10.244.113.156"}:
           .: {}
           f:ip: {}
*/

func IsStatusOnly(field metav1.ManagedFieldsEntry) bool {
	statusOnly := false

	fieldsV1 := field.FieldsV1
	if fieldsV1 == nil {
		return true
	}

	c := make(map[string]json.RawMessage)
	e := json.Unmarshal(field.FieldsV1.Raw, &c)
	if e != nil {
		panic(e)
	}

	for s, _ := range c {
		if s == "f:metadata" || s == "f:spec" {
			return false
		} else if s == "f:status" {
			statusOnly = true
		}
	}

	return statusOnly
}

// WithObject returns a context attached with a Span retrieved from object annotation, it doesn't start a new span
func WithObject(ctx context.Context, meta metav1.Object, obv int64) context.Context {
	var latestContext string
	// var latestTimeStamp *metav1.Time
	var gen int64
	var acontext []string
	var bcontext []string

	managedFields := meta.GetManagedFields()
	for _, mf := range managedFields {
		if IsStatusOnly(mf) {
			continue
		}

		s := strings.Split(mf.TraceContext, "-")
		gen, _ = strconv.ParseInt(s[len(s)-1], 10, 64)
		if gen > obv {
			acontext = append(acontext, mf.TraceContext)
			klog.V(3).InfoS("AAA: Trace request", "object", klog.KObj(meta), "ObG", obv, "Generation", meta.GetGeneration(), "trace-id", mf.TraceContext)
		} else if gen == obv {
			bcontext = append(bcontext, mf.TraceContext)
			klog.V(3).InfoS("BBB: Trace request", "object", klog.KObj(meta), "ObG", obv, "Generation", meta.GetGeneration(), "trace-id", mf.TraceContext)
		} else {
			continue
		}
		/*
			if latestTimeStamp != nil {
				if latestTimeStamp.Before(mf.Time) {
					latestTimeStamp = mf.Time
					latestContext = mf.TraceContext
				}
			} else {
				latestTimeStamp = mf.Time
				latestContext = mf.TraceContext
			}
		*/

		//klog.V(3).InfoS("Trace request", "object", klog.KObj(meta), "ObG", obv, "Generation", meta.GetGeneration(), "trace-id", mf.TraceContext)
	}

	if len(acontext) > 0 {
		latestContext = acontext[0]
	} else if len(bcontext) > 0 {
		latestContext = bcontext[0]
	} else {
		//latestContext = "6617856f277e317fa7aab4c66e0041c9-2aa8325022d99d40-0"
		latestContext = "00000000000000000000000000000001-0000000000000001-0"
		klog.V(3).InfoS("CCC: Trace request", "object", klog.KObj(meta), "ObG", obv, "Generation", meta.GetGeneration(), "trace-id", latestContext)
	}

	span := httpTraceSpan{
		spanContext: stringToSpanContext(latestContext),
	}
	klog.V(3).InfoS("Trace request", "object", klog.KObj(meta), "trace-id", latestContext)
	return apitrace.ContextWithSpan(ctx, span)
	//return spanContextFromAnnotations(ctx, meta, meta.GetAnnotations())

	//ctx, span = StartSpan(ctx)
	//defer span.End()
	//return ctx
}

// spanContextFromAnnotations get span context from annotations
func spanContextFromAnnotations(ctx context.Context, meta metav1.Object, annotations map[string]string) context.Context {
	// get span context from annotations
	spanContext, err := decodeSpanContext(annotations[spanContextAnnotationKey])
	if err != nil {
		return ctx
	}
	span := httpTraceSpan{
		spanContext: spanContext,
	}
	klog.V(3).InfoS("Trace request", "object", klog.KObj(meta), "trace-id", spanContextString(spanContext))
	return apitrace.ContextWithSpan(ctx, span)
}

func spanContextString(spanContext apitrace.SpanContext) string {
	return fmt.Sprintf("%s-%s-%02d", spanContext.TraceID, spanContext.SpanID, spanContext.TraceFlags)
}

func StringSpanContextFromObject(meta metav1.Object) string {
	spanContext, err := decodeSpanContext(meta.GetAnnotations()[spanContextAnnotationKey])
	if err != nil {
		return ""
	}
	return spanContextString(spanContext)
}

// decodeSpanContext decode encodedSpanContext to spanContext
func decodeSpanContext(encodedSpanContext string) (apitrace.SpanContext, error) {
	// decode to byte
	byteList := make([]byte, base64.StdEncoding.DecodedLen(len(encodedSpanContext)))
	l, err := base64.StdEncoding.Decode(byteList, []byte(encodedSpanContext))
	if err != nil {
		return apitrace.EmptySpanContext(), err
	}
	byteList = byteList[:l]
	// decode to span context
	buffer := bytes.NewBuffer(byteList)
	spanContext := apitrace.SpanContext{}
	err = binary.Read(buffer, binary.LittleEndian, &spanContext)
	if err != nil {
		return apitrace.EmptySpanContext(), err
	}
	return spanContext, nil
}

// StartSpan

func StartSpan(ctx context.Context) (context.Context, apitrace.Span) {
	tidlSpan := apitrace.SpanFromContext(ctx)
	klog.Infof("TID-L:%v-%v", tidlSpan.SpanContext().TraceID, tidlSpan.SpanContext().SpanID)
	tp := global.TracerProvider()
	tracer := tp.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "testtest")
	klog.Infof("TID-R:%v-%v", span.SpanContext().TraceID, span.SpanContext().SpanID)
	//return debugContext(ctx, span)
	return ctx, span

}

func debugContext(ctx context.Context, span apitrace.Span) (context.Context, apitrace.Span) {

	ctx = context.WithValue(ctx, contextProcessKey, span.SpanContext().TraceID)
	klog.Infof("check span.TraceID-SpanID-ctx(key) %v-%v-%v", span.SpanContext().TraceID, span.SpanContext().SpanID, ctx.Value(contextProcessKey))
	return ctx, span
}

func UpdateTidList(meta metav1.Object, span apitrace.Span) []metav1.ManagedFieldsEntry {
	klog.Infof("request-check span.TraceID-SpanID %v-%v", span.SpanContext().TraceID, span.SpanContext().SpanID)
	managedFields := meta.GetManagedFields()
	for i, mf := range managedFields {
		if mf.TraceContextProcess == "" {
			mf.TraceContextProcess = span.SpanContext().TraceID.String() + "-" + span.SpanContext().SpanID.String()
			managedFields[i] = mf
			klog.Infof("related TraceID-L %v", mf.TraceContext)
		}
	}
	return managedFields

}

func TraceIDFromContext(ctx context.Context) string {
	if tid, has := ctx.Value(contextProcessKey).(string); has {
		return tid
	}
	return ""
}
