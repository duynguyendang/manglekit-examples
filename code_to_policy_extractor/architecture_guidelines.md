# Architecture Guidelines

## Overview
This document defines the architectural rules for our Go microservices project.
All code must follow these patterns to ensure maintainability and testability.

## Layer Structure
Our application follows Clean Architecture with 4 layers:
1. **controllers** — HTTP handlers, request/response mapping
2. **usecases** — Business logic, application services
3. **domain** — Entities, value objects, repository interfaces
4. **gateways** — External integrations (database, email, APIs)

## Dependency Rules

### Allowed Dependencies
- controllers → usecases (only)
- usecases → domain, gateways
- gateways → domain (only)
- domain → (nothing, must be pure)

### Forbidden Dependencies
- controllers must NOT import domain directly
- controllers must NOT import gateways
- domain must NOT import any other layer
- usecases must NOT import controllers
- No circular dependencies between any layers

## File Naming Conventions

### Controllers
- Must be located in `controllers/` directory
- Must end with `_controller.go`
- Example: `user_controller.go`, `order_controller.go`

### Use Cases
- Must be located in `usecases/` directory
- Must end with `_usecase.go`
- Example: `create_user_usecase.go`, `process_order_usecase.go`

### Domain Entities
- Must be located in `domain/` directory
- Must be singular nouns (e.g., `user.go`, `order.go`)
- Repository interfaces must be in `domain/repositories/`

### Gateways
- Must be located in `gateways/` directory
- Must end with `_gateway.go`
- Example: `postgres_gateway.go`, `email_gateway.go`

## Import Restrictions

### Controllers
```go
// ALLOWED
import "myapp/usecases"

// FORBIDDEN
import "myapp/domain"
import "myapp/gateways"
```

### Domain
```go
// ALLOWED: No imports from other layers
// Only standard library imports allowed
```

## Violation Messages

When a violation is detected, provide:
1. The rule that was violated
2. The file path causing the violation
3. The specific import that is forbidden
4. Suggested fix
