package main

// This is a tool for generating message parser client located under `pb` directory
// NOTE: copy message_parser.proto to the current directly from github.com/redhat-cne/redfish-tools/message-parser/

//go:generate mkdir -p pb
//go:generate protoc --go_out=plugins=grpc:pb --go_opt=paths=source_relative message_parser.proto
