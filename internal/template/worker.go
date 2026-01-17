package template

import "fmt"

// getWorkerFiles returns files for the Worker template
func (g *Generator) getWorkerFiles() ([]File, error) {
	switch g.config.Lang {
	case LangGo:
		return g.getGoWorkerFiles(), nil
	case LangNode:
		return g.getNodeWorkerFiles(), nil
	case LangPython:
		return g.getPythonWorkerFiles(), nil
	default:
		return nil, fmt.Errorf("unsupported language for Worker template: %s", g.config.Lang)
	}
}

// getGoWorkerFiles returns Go worker template files
func (g *Generator) getGoWorkerFiles() []File {
	return []File{
		{
			Path:        "kbox.yaml",
			Description: "kbox configuration",
			Content: `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: {{.Name}}
spec:
  # Workers don't expose ports
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

COPY go.mod go.sum* ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /app/worker .

# Runtime stage
FROM alpine:3.19

WORKDIR /app

COPY --from=builder /app/worker .

RUN adduser -D -u 1000 appuser
USER appuser

CMD ["./worker"]
`,
		},
		{
			Path:        "main.go",
			Description: "Worker process",
			Content: `package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	log.Printf("Starting {{.Name}} worker...")

	// Set up graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("Shutdown signal received")
		cancel()
	}()

	// Main worker loop
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Worker shutting down")
			return
		case <-ticker.C:
			if err := processJob(ctx); err != nil {
				log.Printf("Error processing job: %v", err)
			}
		}
	}
}

func processJob(ctx context.Context) error {
	log.Println("Processing job...")

	// TODO: Add your job processing logic here

	log.Println("Job completed")
	return nil
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
worker
*.exe

# IDE
.idea/
.vscode/
*.swp

# OS
.DS_Store

# Local env
.env
.env.local
`,
		},
	}
}

// getNodeWorkerFiles returns Node.js worker template files
func (g *Generator) getNodeWorkerFiles() []File {
	return []File{
		{
			Path:        "kbox.yaml",
			Description: "kbox configuration",
			Content: `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: {{.Name}}
spec:
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

COPY package*.json ./
RUN npm ci --only=production

COPY . .

USER node

CMD ["node", "worker.js"]
`,
		},
		{
			Path:        "worker.js",
			Description: "Worker process",
			Content: `console.log('Starting {{.Name}} worker...');

// Graceful shutdown
process.on('SIGTERM', () => {
  console.log('Shutdown signal received');
  process.exit(0);
});

// Main worker loop
async function processJob() {
  console.log('Processing job...');

  // TODO: Add your job processing logic here

  console.log('Job completed');
}

setInterval(processJob, 10000);

// Initial run
processJob();
`,
		},
		{
			Path:        "package.json",
			Description: "npm package",
			Content: `{
  "name": "{{.Name}}",
  "version": "1.0.0",
  "main": "worker.js",
  "scripts": {
    "start": "node worker.js"
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

// getPythonWorkerFiles returns Python worker template files
func (g *Generator) getPythonWorkerFiles() []File {
	return []File{
		{
			Path:        "kbox.yaml",
			Description: "kbox configuration",
			Content: `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: {{.Name}}
spec:
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

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY . .

RUN useradd -m -u 1000 appuser
USER appuser

CMD ["python", "worker.py"]
`,
		},
		{
			Path:        "worker.py",
			Description: "Worker process",
			Content: `import signal
import sys
import time
import logging

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


def shutdown_handler(signum, frame):
    logger.info("Shutdown signal received")
    sys.exit(0)


signal.signal(signal.SIGTERM, shutdown_handler)
signal.signal(signal.SIGINT, shutdown_handler)


def process_job():
    logger.info("Processing job...")

    # TODO: Add your job processing logic here

    logger.info("Job completed")


if __name__ == "__main__":
    logger.info("Starting {{.Name}} worker...")

    while True:
        process_job()
        time.sleep(10)
`,
		},
		{
			Path:        "requirements.txt",
			Description: "Python dependencies",
			Content: `# Add your dependencies here
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
