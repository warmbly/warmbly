proto:
	protoc --proto_path=internal/tasks/proto --go_out=internal/tasks/proto --go_opt=paths=source_relative internal/tasks/proto/*.proto
