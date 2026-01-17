package template

import "fmt"

// getAPIFiles returns files for the API template
func (g *Generator) getAPIFiles() ([]File, error) {
	switch g.config.Lang {
	case LangGo:
		return g.getGoAPIFiles(), nil
	case LangNode:
		return g.getNodeAPIFiles(), nil
	case LangPython:
		return g.getPythonAPIFiles(), nil
	default:
		return nil, fmt.Errorf("unsupported language for API template: %s", g.config.Lang)
	}
}

// getGoAPIFiles returns Go API template files
func (g *Generator) getGoAPIFiles() []File {
	return []File{
		{
			Path:        "kbox.yaml",
			Description: "kbox configuration",
			Content: `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: {{.Name}}
spec:
  port: {{.Port}}
  healthCheck: /health
  build:
    dockerfile: Dockerfile
    context: .
  resources:
    memory: 128Mi
    cpu: 100m
{{- if .Database}}
  dependencies:
    - type: {{.Database}}
{{- end}}
`,
		},
		{
			Path:        "Dockerfile",
			Description: "Multi-stage Go build",
			Content: `# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum* ./
RUN go mod download

# Copy source
COPY . .

# Build
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/server .

# Runtime stage
FROM alpine:3.19

WORKDIR /app

# Copy binary
COPY --from=builder /app/server .

# Run as non-root
RUN adduser -D -u 1000 appuser
USER appuser

EXPOSE {{.Port}}

CMD ["./server"]
`,
		},
		{
			Path:        "main.go",
			Description: "HTTP server with /health",
			Content: `package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "{{.Port}}"
	}

	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// Root endpoint
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"service": "{{.Name}}",
			"message": "Hello from {{.Name}}!",
		})
	})

	log.Printf("Starting {{.Name}} on :%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}
`,
		},
		{
			Path:        "go.mod",
			Description: "Go module",
			Content: `module {{.Module}}

go 1.21
`,
		},
		{
			Path:        ".gitignore",
			Description: "Git ignore",
			Content: `# Binaries
{{.Name}}
server
*.exe

# IDE
.idea/
.vscode/
*.swp

# OS
.DS_Store
Thumbs.db

# Local env
.env
.env.local
`,
		},
	}
}

// getNodeAPIFiles returns Node.js API template files
func (g *Generator) getNodeAPIFiles() []File {
	return []File{
		{
			Path:        "kbox.yaml",
			Description: "kbox configuration",
			Content: `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: {{.Name}}
spec:
  port: {{.Port}}
  healthCheck: /health
  build:
    dockerfile: Dockerfile
    context: .
  resources:
    memory: 256Mi
    cpu: 100m
{{- if .Database}}
  dependencies:
    - type: {{.Database}}
{{- end}}
`,
		},
		{
			Path:        "Dockerfile",
			Description: "Node.js build",
			Content: `FROM node:20-alpine

WORKDIR /app

# Copy package files
COPY package*.json ./
RUN npm ci --only=production

# Copy source
COPY . .

# Run as non-root
USER node

EXPOSE {{.Port}}

CMD ["node", "index.js"]
`,
		},
		{
			Path:        "index.js",
			Description: "Express server",
			Content: `const express = require('express');
const app = express();

const PORT = process.env.PORT || {{.Port}};

// Health check
app.get('/health', (req, res) => {
  res.json({ status: 'ok' });
});

// Root endpoint
app.get('/', (req, res) => {
  res.json({
    service: '{{.Name}}',
    message: 'Hello from {{.Name}}!'
  });
});

app.listen(PORT, () => {
  console.log(` + "`" + `{{.Name}} listening on port ${PORT}` + "`" + `);
});
`,
		},
		{
			Path:        "package.json",
			Description: "npm package",
			Content: `{
  "name": "{{.Name}}",
  "version": "1.0.0",
  "main": "index.js",
  "scripts": {
    "start": "node index.js",
    "dev": "node --watch index.js"
  },
  "dependencies": {
    "express": "^4.18.2"
  }
}
`,
		},
		{
			Path:        ".gitignore",
			Description: "Git ignore",
			Content: `node_modules/
.env
.env.local
.DS_Store
*.log
`,
		},
	}
}

// getPythonAPIFiles returns Python API template files
func (g *Generator) getPythonAPIFiles() []File {
	return []File{
		{
			Path:        "kbox.yaml",
			Description: "kbox configuration",
			Content: `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: {{.Name}}
spec:
  port: {{.Port}}
  healthCheck: /health
  build:
    dockerfile: Dockerfile
    context: .
  resources:
    memory: 256Mi
    cpu: 100m
{{- if .Database}}
  dependencies:
    - type: {{.Database}}
{{- end}}
`,
		},
		{
			Path:        "Dockerfile",
			Description: "Python build",
			Content: `FROM python:3.12-slim

WORKDIR /app

# Install dependencies
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Copy source
COPY . .

# Run as non-root
RUN useradd -m -u 1000 appuser
USER appuser

EXPOSE {{.Port}}

CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "{{.Port}}"]
`,
		},
		{
			Path:        "main.py",
			Description: "FastAPI server",
			Content: `from fastapi import FastAPI

app = FastAPI(title="{{.Name}}")


@app.get("/health")
def health():
    return {"status": "ok"}


@app.get("/")
def root():
    return {
        "service": "{{.Name}}",
        "message": "Hello from {{.Name}}!"
    }
`,
		},
		{
			Path:        "requirements.txt",
			Description: "Python dependencies",
			Content: `fastapi==0.109.0
uvicorn[standard]==0.27.0
`,
		},
		{
			Path:        ".gitignore",
			Description: "Git ignore",
			Content: `__pycache__/
*.py[cod]
.env
.env.local
.venv/
venv/
.DS_Store
`,
		},
	}
}
