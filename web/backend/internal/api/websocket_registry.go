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

func toSnakeCase(s string) string {
	var res strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			prev := s[i-1]
			if (prev >= 'a' && prev <= 'z') || (prev >= '0' && prev <= '9') {
				res.WriteRune('_')
			}
		}
		res.WriteRune(r)
	}
	return strings.ToLower(res.String())
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

		var respTyp reflect.Type
		if methodTyp.NumOut() > 0 {
			respTyp = methodTyp.Out(0)
		}

		methodNameLower := strings.ToLower(method.Name)
		methodNameSnake := toSnakeCase(method.Name)

		if prefix == "" {
			structNameRaw := reflect.Indirect(val).Type().Name()
			structNameLower := strings.ToLower(structNameRaw)
			structNameSnake := toSnakeCase(structNameRaw)

			// Generate all combinations of structName and methodName
			routes := []string{
				fmt.Sprintf("%s::%s", structNameLower, methodNameLower),
			}
			route2 := fmt.Sprintf("%s::%s", structNameLower, methodNameSnake)
			if route2 != routes[0] {
				routes = append(routes, route2)
			}
			route3 := fmt.Sprintf("%s::%s", structNameSnake, methodNameLower)
			if route3 != routes[0] && route3 != route2 {
				routes = append(routes, route3)
			}
			route4 := fmt.Sprintf("%s::%s", structNameSnake, methodNameSnake)
			if route4 != routes[0] && route4 != route2 && route4 != route3 {
				routes = append(routes, route4)
			}

			for _, route := range routes {
				w.addRoute(route, val, methodTyp.In(3), respTyp, method.Func)
			}
		} else {
			// prefix is already built as a lowercase path
			route1 := fmt.Sprintf("%s::%s", prefix, methodNameLower)
			route2 := fmt.Sprintf("%s::%s", prefix, methodNameSnake)

			w.addRoute(route1, val, methodTyp.In(3), respTyp, method.Func)
			if route2 != route1 {
				w.addRoute(route2, val, methodTyp.In(3), respTyp, method.Func)
			}
		}
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
