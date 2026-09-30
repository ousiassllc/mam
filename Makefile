.DEFAULT_GOAL := help

.PHONY: help dev build generate assets test test-go test-static lint lint-go lint-static fmt fmt-check tools clean

help: ## show targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-14s %s\n", $$1, $$2}'

dev: ## watch mode: air (Go) + templ generate --watch + esbuild/tailwind --watch
	@test -d static/node_modules || npm ci --prefix static
	@MAM_STATIC_DIR=static/src/dist ./static/node_modules/.bin/concurrently -k -n air,templ,static \
		"air" \
		"templ generate --watch" \
		"npm run dev --prefix static"

build: ## production build into bin/mam (templ -> esbuild -> tailwind -> go build)
	./scripts/build.sh

build-all: ## production build for every platform (windows/darwin/linux x amd64/arm64) into bin/mam-<os>-<arch>
	./scripts/build.sh --all

generate: ## templ generate
	templ generate ./backend/...

assets: ## build front-end assets into static/src/dist
	npm run build --prefix static

test: test-go test-static ## run every test

test-go: generate ## Go tests
	go test -race ./...

test-static: ## front-end unit tests
	npm test --prefix static

lint: lint-go lint-static ## golangci-lint + tsc

lint-go: generate
	golangci-lint run ./...

lint-static:
	npm run typecheck --prefix static

fmt: ## format Go code and templ templates
	templ fmt backend
	golangci-lint fmt ./...

fmt-check: ## fail when Go code or templates are not formatted
	@test -z "$$(gofmt -l backend static)" || (gofmt -l backend static; exit 1)
	@templ fmt -fail backend

tools: ## install dev tools (templ, air, golangci-lint must be on PATH) and npm deps
	go install github.com/a-h/templ/cmd/templ@latest
	go install github.com/air-verse/air@latest
	npm ci --prefix static

clean:
	rm -rf bin tmp static/src/dist/app.*
