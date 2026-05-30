package api

import (
	"fmt"
	"reflect"
	"strings"
	"sync"

	"google.golang.org/protobuf/proto"
)

type WSRegistration struct {
	Receiver     reflect.Value // <-- Added: Storing the actual 'b' instance
	RequestType  reflect.Type  // Storing the type of proto.Message (e.g., reflect.TypeOf(&apiproto.BillList{}))
	ResponseType reflect.Type
	HandlerFunc  reflect.Value // Storing the method function itself
}

type WSRegistry struct {
	mu       sync.RWMutex
	handlers map[string]WSRegistration
}

func NewWSRegistry() *WSRegistry {
	return &WSRegistry{
		handlers: make(map[string]WSRegistration),
	}
}

func (w *WSRegistry) Register(rcvr interface{}) {
	val := reflect.ValueOf(rcvr)
	// Start scanning from an empty string prefix
	w.registerRecursive("", val)
}

func (w *WSRegistry) registerRecursive(prefix string, val reflect.Value) {
	typ := val.Type()

	// 1. Process exported methods directly tied to this level
	for i := 0; i < typ.NumMethod(); i++ {
		method := typ.Method(i)
		methodTyp := method.Type

		// Ensure argument signature matches: receiver + session + reqID + body
		if methodTyp.NumIn() != 4 {
			continue
		}

		methodName := strings.ToLower(method.Name)
		var fullRoute string
		if prefix == "" {
			// e.g. "auth::handshake"
			structName := strings.ToLower(reflect.Indirect(val).Type().Name())
			fullRoute = fmt.Sprintf("%s::%s", structName, methodName)
		} else {
			// e.g. "auth::register::begin"
			fullRoute = fmt.Sprintf("%s::%s", prefix, methodName)
		}

		var respTyp reflect.Type
		if methodTyp.NumOut() > 0 {
			respTyp = methodTyp.Out(0)
		}

		w.addRoute(fullRoute, val, methodTyp.In(3), respTyp, method.Func)
	}

	// 2. Drill down into nested struct pointers to establish sub-namespaces
	// Indirect checks actual struct layout underlying potential pointers
	indirectVal := reflect.Indirect(val)
	if indirectVal.Kind() == reflect.Struct {
		indTyp := indirectVal.Type()

		for i := 0; i < indirectVal.NumField(); i++ {
			fieldVal := indirectVal.Field(i)
			fieldTyp := indTyp.Field(i)

			// Only traverse into initialized fields (pointers or structs)
			if fieldVal.Kind() == reflect.Ptr && !fieldVal.IsNil() {
				fieldName := strings.ToLower(fieldTyp.Name)

				var nextPrefix string
				if prefix == "" {
					structName := strings.ToLower(indTyp.Name())
					nextPrefix = fmt.Sprintf("%s::%s", structName, fieldName)
				} else {
					nextPrefix = fmt.Sprintf("%s::%s", prefix, fieldName)
				}

				// Recurse into nested struct field!
				w.registerRecursive(nextPrefix, fieldVal)
			}
		}
	}
}

func (r *WSRegistry) addRoute(path string, receiver reflect.Value, req reflect.Type, resp reflect.Type, handler reflect.Value) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.handlers[path] = WSRegistration{
		Receiver:     receiver, // Track which instance owns this method
		RequestType:  req,
		ResponseType: resp,
		HandlerFunc:  handler,
	}
}

func (r *WSRegistry) Get(path string) (WSRegistration, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	reg, ok := r.handlers[path]
	return reg, ok
}

// Helper to create a new instance of a proto message for unmarshaling
func CreateProtoInstance(p proto.Message) proto.Message {
	if p == nil {
		return nil
	}
	return reflect.New(reflect.TypeOf(p).Elem()).Interface().(proto.Message)
}
