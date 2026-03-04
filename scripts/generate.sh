#!/bin/bash
set -e

echo "Generating Protobuf stubs and OpenAPI spec..."
buf generate

echo "Moving generated Go models to api/v1/..."
mkdir -p ./api/v1
mv ./api/proto/v1/*.go ./api/v1/

echo "Moving generated Swagger specs to api/swagger/..."
mkdir -p ./api/swagger
mv ./api/swagger/api/proto/v1/*.json ./api/swagger/ 2>/dev/null || true
rm -rf ./api/swagger/api

echo "Code generation complete!"
