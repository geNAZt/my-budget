package backend

//go:generate oapi-codegen -config pkg/apis/gocardless/client.cfg.yaml apis/gocardless.json
//go:generate oapi-codegen -config pkg/apis/trading212/client.cfg.yaml apis/trading212.json
//go:generate oapi-codegen -config pkg/apis/enablebanking/client.cfg.yaml apis/enablebanking.json

//go:generate buf generate --template buf.gen.go.yaml ../proto/api.proto
//go:generate buf generate --template buf.gen.execution.yaml execution_engine/proto/execution.proto
