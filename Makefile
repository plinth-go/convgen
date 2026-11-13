GO ?= go
GOFMT ?= gofmt
GO_LICENSES_VERSION ?= v1.6.0
ACTIONLINT_VERSION ?= v1.7.12
ALLOWED_LICENSES ?= Apache-2.0,MIT,BSD-2-Clause,BSD-3-Clause,BSL-1.0,Unlicense

.PHONY: all hooks check fmt generate generate-check dco-check test test-race vet build license-check license-update notices-check lint-workflows quality release-check clean verify

all: check build

hooks:
	git config --local core.hooksPath .githooks

check:
	@test -z "$$($(GOFMT) -l .)" || (echo "convgen: 存在未格式化文件，请运行 make fmt" && exit 1)
	$(GO) vet ./...
	$(GO) test -count=1 ./...

fmt:
	$(GOFMT) -w .

generate:
	$(GO) run . ./...

generate-check:
	$(GO) run . ./...
	@test -z "$$(git status --porcelain --untracked-files=all -- '*conv_gen.go')" \
		|| (echo "convgen: 生成文件与声明不同步，请运行 make generate" && exit 1)

test:
	$(GO) test -count=1 ./...

test-race:
	$(GO) test -race -shuffle=on -count=1 ./...

dco-check:
	./scripts/check-dco.sh "$${DCO_BASE:-origin/main}" HEAD

vet:
	$(GO) vet ./...

build:
	mkdir -p bin
	$(GO) build -trimpath -o bin/convgen .

license-check:
	GO="$(GO)" GO_LICENSES_VERSION="$(GO_LICENSES_VERSION)" ./scripts/go-licenses.sh check --include_tests --ignore github.com/plinth-go/convgen --allowed_licenses=$(ALLOWED_LICENSES) ./...

license-update:
	GO="$(GO)" GO_LICENSES_VERSION="$(GO_LICENSES_VERSION)" ./scripts/update-third-party-notices.sh

notices-check:
	GO="$(GO)" GO_LICENSES_VERSION="$(GO_LICENSES_VERSION)" ./scripts/update-third-party-notices.sh
	@test -z "$$(git status --porcelain --untracked-files=all -- THIRD_PARTY_NOTICES.md)" \
		|| (echo "convgen: 第三方许可清单未同步，请提交 make license-update 的结果" && exit 1)

lint-workflows:
	$(GO) run github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION) .github/workflows/*.yml

quality: license-check notices-check lint-workflows

release-check: verify quality

clean:
	rm -rf bin

verify: check test-race generate-check build quality
