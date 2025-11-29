# Stargate Development Guide

## Build Commands

### Frontend (React)
```bash
cd frontend
npm install
npm start          # Dev server on localhost:3000
npm run build      # Production build
npm test           # Run Jest tests
npm test -- --testNamePattern="SpecificTest"  # Run single test
```

### Backend (Go)
```bash
cd backend
go mod tidy        # Install dependencies
go run stargate_backend.go  # Dev server on localhost:3001
go build           # Build binary
go test ./...      # Run all tests (when implemented)
go test -run TestSpecificFunction  # Run single test
```

## Code Style Guidelines

### Frontend (React/JavaScript)
- **Components**: PascalCase (BlockCard, InscriptionModal)
- **Files**: PascalCase.js for components
- **Hooks**: camelCase with use prefix (useBlocks, useInscriptions)
- **Constants**: UPPER_SNAKE_CASE
- **Functions**: camelCase
- **Imports**: ES6 imports, React hooks first
- **Error Handling**: Try-catch with user-friendly messages
- **State Management**: React hooks (useState, useEffect, useCallback)

### Backend (Go)
- **Packages**: lowercase, single word (handlers, services, models)
- **Files**: snake_case.go (data_storage.go, block_handler.go)
- **Structs**: PascalCase (BlockData, InscriptionService)
- **Functions**: PascalCase for exported, camelCase for unexported
- **Constants**: UPPER_SNAKE_CASE or PascalCase for exported
- **Error Handling**: Explicit error returns, wrap errors with context
- **Imports**: Grouped (stdlib, third-party, local packages)

## Testing

### Frontend
- **Framework**: Jest with React Testing Library
- **Test Files**: *.test.js alongside components
- **Run Single**: `npm test -- --testNamePattern="TestName"`

### Backend
- **Framework**: Go testing package
- **Test Files**: *_test.go alongside source files
- **Run Single**: `go test -run TestFunctionName`

## Linting/Formatting

### Frontend
- **ESLint**: Configured via package.json (react-app preset)
- **Prettier**: Uses Create React App defaults
- **Fix**: `npm run lint` (if configured)

### Backend
- **Format**: `go fmt ./...`
- **Lint**: `golint ./...` (if installed)
- **Vet**: `go vet ./...`

## Key Patterns

### Frontend
- Use custom hooks for API calls and state management
- Component composition over inheritance
- Tailwind classes for styling (no inline styles)
- Proper error boundaries and loading states

### Backend
- Dependency injection via container pattern
- Middleware chain for cross-cutting concerns
- File-based storage with proper error handling
- RESTful API design with JSON responses

## Development Workflow

1. Start backend: `cd backend && go run stargate_backend.go &`
2. Start frontend: `cd frontend && npm start`
3. Backend runs on :3001, Frontend on :3000
4. Use background processes for long-running servers