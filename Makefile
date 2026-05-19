VERSION ?= $(shell tr -d '[:space:]' < VERSION)
TAG := v$(VERSION)

.PHONY: help test docs-build docs-dev check-version release-tag release

help:
	@echo "godantic release commands"
	@echo ""
	@echo "  make test              Run Go tests"
	@echo "  make docs-build        Build Starlight docs"
	@echo "  make docs-dev          Run docs dev server"
	@echo "  make release-tag       Create git tag from VERSION"
	@echo "  make release           Test, build docs, tag, push, create GitHub release"
	@echo ""
	@echo "Current version: $(TAG)"

test:
	go test ./...

docs-build:
	cd docs && npm ci && npm run build

docs-dev:
	cd docs && npm install && npm run dev

check-version:
	@test -n "$(VERSION)" || (echo "VERSION is empty" && exit 1)
	@echo "$(TAG)" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$$' || (echo "Invalid VERSION: $(VERSION)" && exit 1)
	@git diff --quiet || (echo "Working tree has unstaged changes" && exit 1)
	@git diff --cached --quiet || (echo "Index has staged changes" && exit 1)
	@! git rev-parse "$(TAG)" >/dev/null 2>&1 || (echo "Tag $(TAG) already exists" && exit 1)

release-tag: check-version
	git tag -a "$(TAG)" -m "Release $(TAG)"

release: test docs-build check-version
	git tag -a "$(TAG)" -m "Release $(TAG)"
	git push origin main
	git push origin "$(TAG)"
	gh release create "$(TAG)" --title "$(TAG)" --notes-file CHANGELOG.md
