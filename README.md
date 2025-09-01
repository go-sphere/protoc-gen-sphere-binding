# protoc-gen-sphere-binding
`protoc-gen-sphere-binding` is a protoc plugin that generates Go struct tags for Sphere binding from `.proto` files. It is designed to inspect service definitions within your protobuf files and automatically generate corresponding Go struct tags based on a specified template. Inspired by [protoc-gen-gotag](https://github.com/srikrsna/protoc-gen-gotag).


## Installation

To install `protoc-gen-sphere-binding`, use the following command:

```bash
go install github.com/go-sphere/protoc-gen-sphere-binding@latest
```


## Flags
The behavior of `protoc-gen-sphere-binding` can be customized with the following parameters:
- **`version`**: Print the current plugin version and exit. (Default: `false`)
- **`out`**: The output directory for the modified `.proto` files. (Default: `api`)


## Usage with Buf

To use `protoc-gen-sphere-binding` with `buf`, you can configure it in your `buf.binding.yaml` file. `protoc-gen-sphere-binding` can not be used with `buf.gen.yaml` because it does not generate Go code, but rather modifies the `.proto` files to include Sphere binding tags. Here is an example configuration:

```yaml
version: v2
managed:
  enabled: true
  override:
    - file_option: go_package_prefix
      value: github.com/go-sphere/sphere-layout/api
plugins:
  - local: protoc-gen-sphere-binding
    out: api
    opt:
      - paths=source_relative
      - out=api

```


## Prerequisites

You need to have the sphere binding proto definitions in your project. Add the following dependency to your `buf.yaml`:

```yaml
deps:
  - buf.build/go-sphere/binding
```

## How It Works

`protoc-gen-sphere-binding` processes protobuf files and adds Go struct tags based on sphere binding annotations. Unlike other protoc plugins that generate new files, this plugin modifies the generated Go structs by injecting appropriate tags for request binding in HTTP handlers.

The plugin works in conjunction with `protoc-gen-go` and should be run after the standard Go code generation to add the binding tags to the generated structs.

## Binding Locations

The plugin supports the following binding locations through the `sphere.binding.location` annotation:

- `BINDING_LOCATION_BODY`: Fields bound to JSON request body (default behavior)
- `BINDING_LOCATION_QUERY`: Fields bound to query parameters (adds `form` tag)
- `BINDING_LOCATION_URI`: Fields bound to URI path parameters (adds `uri` tag, removes `json` tag)

## Proto Definition Example

Here's a comprehensive example showing different binding locations:

```protobuf
syntax = "proto3";

package shared.v1;

import "buf/validate/validate.proto";
import "google/api/annotations.proto";
import "sphere/binding/binding.proto";

service TestService {
  rpc RunTest(RunTestRequest) returns (RunTestResponse) {
    option (google.api.http) = {
      post: "/api/test/{path_test1}/second/{path_test2}"
      body: "*"
    };
  }
}

message RunTestRequest {
  // Fields without binding annotation go to JSON body
  string field_test1 = 1;
  int64 field_test2 = 2;
  
  // URI path parameters
  string path_test1 = 3 [(sphere.binding.location) = BINDING_LOCATION_URI];
  int64 path_test2 = 4 [(sphere.binding.location) = BINDING_LOCATION_URI];
  
  // Query parameters
  string query_test1 = 5 [
    (buf.validate.field).required = true,
    (sphere.binding.location) = BINDING_LOCATION_QUERY
  ];
  int64 query_test2 = 6 [(sphere.binding.location) = BINDING_LOCATION_QUERY];
  
  // Query parameter with custom tag
  repeated TestEnum enum_test1 = 7 [
    (sphere.binding.location) = BINDING_LOCATION_QUERY,
    (sphere.binding.tags) = "sphere:\"enum_test1\""
  ];
  
  // Header binding
  string auth_token = 8 [(sphere.binding.location) = BINDING_LOCATION_HEADER];
}

// Message with default auto tags
message BodyPathTestRequest {
  message Request {
    option (sphere.binding.default_auto_tags) = "db";
    string field_test1 = 1;
    int64 field_test2 = 2;
  }
  Request request = 1;
}

enum TestEnum {
  TEST_ENUM_UNSPECIFIED = 0;
  TEST_ENUM_VALUE1 = 1;
  TEST_ENUM_VALUE2 = 2;
}
```

## Generated Code

After running `protoc-gen-go` followed by `protoc-gen-sphere-binding`, the generated Go struct will have appropriate tags:

```go
type RunTestRequest struct {
    state         protoimpl.MessageState `protogen:"open.v1"`
    // JSON body fields (default behavior)
    FieldTest1    string                 `protobuf:"bytes,1,opt,name=field_test1,json=fieldTest1,proto3" json:"field_test1,omitempty"`
    FieldTest2    int64                  `protobuf:"varint,2,opt,name=field_test2,json=fieldTest2,proto3" json:"field_test2,omitempty"`
    
    // URI path parameters (json tag removed, uri tag added)
    PathTest1     string                 `protobuf:"bytes,3,opt,name=path_test1,json=pathTest1,proto3" json:"-" uri:"path_test1"`
    PathTest2     int64                  `protobuf:"varint,4,opt,name=path_test2,json=pathTest2,proto3" json:"-" uri:"path_test2"`
    
    // Query parameters (json tag removed, form tag added)
    QueryTest1    string                 `protobuf:"bytes,5,opt,name=query_test1,json=queryTest1,proto3" json:"-" form:"query_test1"`
    QueryTest2    int64                  `protobuf:"varint,6,opt,name=query_test2,json=queryTest2,proto3" json:"-" form:"query_test2"`
    
    // Query parameter with custom sphere tag
    EnumTest1     []TestEnum             `protobuf:"varint,7,rep,packed,name=enum_test1,json=enumTest1,proto3,enum=shared.v1.TestEnum" json:"-" form:"enum_test1" sphere:"enum_test1"`
    
    // Header binding
    AuthToken     string                 `protobuf:"bytes,8,opt,name=auth_token,json=authToken,proto3" json:"-" header:"auth_token"`
    
    unknownFields protoimpl.UnknownFields
    sizeCache     protoimpl.SizeCache
}

type BodyPathTestRequest_Request struct {
    state         protoimpl.MessageState `protogen:"open.v1"`
    FieldTest1    string                 `protobuf:"bytes,1,opt,name=field_test1,json=fieldTest1,proto3" json:"field_test1,omitempty" db:"field_test1"`
    FieldTest2    int64                  `protobuf:"varint,2,opt,name=field_test2,json=fieldTest2,proto3" json:"field_test2,omitempty" db:"field_test2"`
    unknownFields protoimpl.UnknownFields
    sizeCache     protoimpl.SizeCache
}
```

## Usage in HTTP Handlers

The generated tags work seamlessly with Gin's binding functions:

```go
func handler(ctx *gin.Context) {
    var req RunTestRequest
    
    // Bind JSON body
    if err := ctx.ShouldBindJSON(&req); err != nil {
        // handle error
    }
    
    // Bind query parameters
    if err := ctx.ShouldBindQuery(&req); err != nil {
        // handle error
    }
    
    // Bind URI parameters
    if err := ctx.ShouldBindUri(&req); err != nil {
        // handle error
    }
    
    // Bind headers
    if err := ctx.ShouldBindHeader(&req); err != nil {
        // handle error
    }
}
```

## Advanced Features

### Custom Tags

You can specify custom tags using the `sphere.binding.tags` annotation:

```protobuf
message CustomTagExample {
  string field1 = 1 [
    (sphere.binding.location) = BINDING_LOCATION_QUERY,
    (sphere.binding.tags) = "validate:\"required\" custom:\"value\""
  ];
}
```

### Default Auto Tags

For messages that need default tags on all fields, use `default_auto_tags`:

```protobuf
message DatabaseModel {
  option (sphere.binding.default_auto_tags) = "db";
  
  string name = 1;      // Will get db:"name" tag
  int64 id = 2;         // Will get db:"id" tag
}
```

### Tag Override Behavior

- `BINDING_LOCATION_URI` and `BINDING_LOCATION_QUERY`: Removes `json` tag and adds respective binding tag
- `BINDING_LOCATION_HEADER`: Removes `json` tag and adds `header` tag  
- `BINDING_LOCATION_FORM`: Removes `json` tag and adds `form` tag
- `BINDING_LOCATION_BODY`: Keeps original `json` tag (default behavior)

## Integration with protoc-gen-sphere

This plugin works perfectly with `protoc-gen-sphere` to create complete HTTP handlers:

1. `protoc-gen-go` generates base Go structs
2. `protoc-gen-sphere-binding` adds binding tags to structs
3. `protoc-gen-sphere` generates HTTP handlers that use the tagged structs

## Best Practices

1. **Use meaningful field names**: Field names become tag values, so use clear, descriptive names
2. **Validate URI parameters**: Always validate path parameters since they come from untrusted input
3. **Group related bindings**: Keep related query parameters together in your proto definition
4. **Document your APIs**: Use comments in proto files as they may be used in generated documentation
5. **Consistent naming**: Use consistent naming patterns for similar parameters across your API

## Common Use Cases

### REST API with Path Parameters

```protobuf
rpc GetUser(GetUserRequest) returns (GetUserResponse) {
  option (google.api.http) = {get: "/api/users/{user_id}"};
}

message GetUserRequest {
  int64 user_id = 1 [(sphere.binding.location) = BINDING_LOCATION_URI];
}
```

### Search with Query Parameters

```protobuf
rpc SearchUsers(SearchUsersRequest) returns (SearchUsersResponse) {
  option (google.api.http) = {get: "/api/users/search"};
}

message SearchUsersRequest {
  string query = 1 [(sphere.binding.location) = BINDING_LOCATION_QUERY];
  int32 limit = 2 [(sphere.binding.location) = BINDING_LOCATION_QUERY];
  int32 offset = 3 [(sphere.binding.location) = BINDING_LOCATION_QUERY];
}
```

### Mixed Body and Path Parameters

```protobuf
rpc UpdateUser(UpdateUserRequest) returns (UpdateUserResponse) {
  option (google.api.http) = {
    put: "/api/users/{user_id}"
    body: "user"
  };
}

message UpdateUserRequest {
  int64 user_id = 1 [(sphere.binding.location) = BINDING_LOCATION_URI];
  User user = 2;  // Goes to JSON body
}
```