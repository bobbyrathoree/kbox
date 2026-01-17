package template

import "fmt"

// getCronFiles returns files for the Cron template
func (g *Generator) getCronFiles() ([]File, error) {
	switch g.config.Lang {
	case LangGo:
		return g.getGoCronFiles(), nil
	case LangNode:
		return g.getNodeCronFiles(), nil
	case LangPython:
		return g.getPythonCronFiles(), nil
	default:
		return nil, fmt.Errorf("unsupported language for Cron template: %s", g.config.Lang)
	}
}

// getGoCronFiles returns Go cron template files
func (g *Generator) getGoCronFiles() []File {
	return []File{
		{
			Path:        "kbox.yaml",
			Description: "kbox configuration with CronJob",
			Content: `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: {{.Name}}
spec:
  build:
    dockerfile: Dockerfile
    context: .
  resources:
    memory: 128Mi
    cpu: 100m
  jobs:
    - name: {{.Name}}-task
      schedule: "*/5 * * * *"  # Every 5 minutes
      command: ["./task"]
      backoffLimit: 3
      ttlSecondsAfterFinished: 3600
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

RUN CGO_ENABLED=0 GOOS=linux go build -o /app/task .

# Runtime stage
FROM alpine:3.19

WORKDIR /app

COPY --from=builder /app/task .

RUN adduser -D -u 1000 appuser
USER appuser

CMD ["./task"]
`,
		},
		{
			Path:        "main.go",
			Description: "Cron task",
			Content: `package main

import (
	"log"
	"os"
)

func main() {
	log.Printf("Starting {{.Name}} task...")

	if err := run(); err != nil {
		log.Printf("Task failed: %v", err)
		os.Exit(1)
	}

	log.Println("Task completed successfully")
}

func run() error {
	// TODO: Add your task logic here

	log.Println("Processing...")

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
task
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

// getNodeCronFiles returns Node.js cron template files
func (g *Generator) getNodeCronFiles() []File {
	return []File{
		{
			Path:        "kbox.yaml",
			Description: "kbox configuration with CronJob",
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
  jobs:
    - name: {{.Name}}-task
      schedule: "*/5 * * * *"  # Every 5 minutes
      command: ["node", "task.js"]
      backoffLimit: 3
      ttlSecondsAfterFinished: 3600
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

CMD ["node", "task.js"]
`,
		},
		{
			Path:        "task.js",
			Description: "Cron task",
			Content: `console.log('Starting {{.Name}} task...');

async function run() {
  // TODO: Add your task logic here

  console.log('Processing...');
}

run()
  .then(() => {
    console.log('Task completed successfully');
    process.exit(0);
  })
  .catch((err) => {
    console.error('Task failed:', err);
    process.exit(1);
  });
`,
		},
		{
			Path:        "package.json",
			Description: "npm package",
			Content: `{
  "name": "{{.Name}}",
  "version": "1.0.0",
  "main": "task.js",
  "scripts": {
    "start": "node task.js"
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

// getPythonCronFiles returns Python cron template files
func (g *Generator) getPythonCronFiles() []File {
	return []File{
		{
			Path:        "kbox.yaml",
			Description: "kbox configuration with CronJob",
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
  jobs:
    - name: {{.Name}}-task
      schedule: "*/5 * * * *"  # Every 5 minutes
      command: ["python", "task.py"]
      backoffLimit: 3
      ttlSecondsAfterFinished: 3600
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

CMD ["python", "task.py"]
`,
		},
		{
			Path:        "task.py",
			Description: "Cron task",
			Content: `import sys
import logging

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


def run():
    # TODO: Add your task logic here

    logger.info("Processing...")


if __name__ == "__main__":
    logger.info("Starting {{.Name}} task...")

    try:
        run()
        logger.info("Task completed successfully")
    except Exception as e:
        logger.error(f"Task failed: {e}")
        sys.exit(1)
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
