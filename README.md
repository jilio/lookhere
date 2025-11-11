# LOOKHERE - Go Client Library

Public Go client library for [LOOKHERE](https://lookhere.tech) - Event sourcing as a service.

## Installation

```bash
go get github.com/jilio/lookhere
```

## Usage

Connect your EventBus to LOOKHERE cloud storage:

```go
package main

import (
    eventbus "github.com/jilio/ebu"
    "github.com/jilio/lookhere"
)

func main() {
    // Get your DSN from https://lookhere.tech
    dsn := "grpc://your-api-key@lookhere.tech"
    
    // Create EventBus with cloud storage
    bus := eventbus.New(lookhere.WithCloud(dsn))
    
    // Your events are now persisted to LOOKHERE!
}
```

## DSN Format

```
grpc://api-key@host
```

- `api-key`: Your LOOKHERE API key (get from dashboard)
- `host`: LOOKHERE server host (usually `lookhere.tech`)

## Features

- **Automatic persistence**: Events are automatically saved to cloud storage
- **Subscription tracking**: Subscription positions are maintained across restarts
- **Simple integration**: Single function call to enable cloud storage

## Requirements

- Go 1.24.2 or later
- [github.com/jilio/ebu](https://github.com/jilio/ebu) v0.8.4 or later

## License

MIT License - See LICENSE file for details
