package api

import (
	"testing"

	"google.golang.org/protobuf/proto"
)

type mockProto struct {
	proto.Message
}

type MockHandler struct {
	IntField int
}

func (h *MockHandler) NoReturn(s *WebsocketSession, reqID string, body proto.Message) {}

func (h *MockHandler) WithReturn(s *WebsocketSession, reqID string, body proto.Message) proto.Message {
	return &mockProto{}
}

func (h *MockHandler) WrongArgs(s *WebsocketSession, reqID string) {}

func (h *MockHandler) CamelCaseMethod(s *WebsocketSession, reqID string, body proto.Message) {}

func TestWSRegistry_Register(t *testing.T) {
	registry := NewWSRegistry()
	handler := &MockHandler{}

	// This should not panic
	t.Run("Register NoReturn", func(t *testing.T) {
		registry.Register(handler)

		_, ok := registry.Get("mockhandler::noreturn")
		if !ok {
			t.Errorf("expected mockhandler::noreturn to be registered")
		}
	})

	t.Run("Register WithReturn", func(t *testing.T) {
		_, ok := registry.Get("mockhandler::withreturn")
		if !ok {
			t.Errorf("expected mockhandler::withreturn to be registered")
		}
	})

	t.Run("Ignore WrongArgs", func(t *testing.T) {
		_, ok := registry.Get("mockhandler::wrongargs")
		if ok {
			t.Errorf("expected mockhandler::wrongargs to be ignored")
		}
	})

	t.Run("Register CamelCaseMethod", func(t *testing.T) {
		_, okLower := registry.Get("mockhandler::camelcasemethod")
		if !okLower {
			t.Errorf("expected mockhandler::camelcasemethod to be registered")
		}

		_, okSnake := registry.Get("mockhandler::camel_case_method")
		if !okSnake {
			t.Errorf("expected mockhandler::camel_case_method to be registered")
		}
	})
}
