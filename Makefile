GO ?= go
# Where sandpitd keeps everything (firecracker, kernel, images, initrd, sprites,
# token). DATA=<dir> on the command line overrides it for every target here; a
# DATA (or NAME, FLAGS) that is merely in the environment is ignored, since those
# are common enough variable names (WSL sets NAME) that a plain run must not
# pick them up.
SANDPIT_DATA ?= $(or $(XDG_DATA_HOME),$(HOME)/.local/share)/sandpit
ifeq ($(origin DATA),command line)
SANDPIT_DATA := $(DATA)
endif
export SANDPIT_DATA
override NAME := $(if $(filter command line,$(origin NAME)),$(NAME),sandpit)
override FLAGS := $(if $(filter command line,$(origin FLAGS)),$(FLAGS),)
VARIANTS ?= base e2b vercel daytona modal

.PHONY: all build netd deps image images initrd run install-service test test-scripts e2e

# Nothing here writes into wisp's data directory: an initrd or disk image built
# there would replace what wispd boots. (A sandboxd data directory being taken
# over is fine: see install-service.)
WISPS_DATA_DIRS := $(abspath $(or $(XDG_DATA_HOME),$(HOME)/.local/share)/wisp $(HOME)/.local/share/wisp)
ifneq ($(filter all deps image images initrd run install-service,$(or $(MAKECMDGOALS),all)),)
ifneq ($(filter $(abspath $(SANDPIT_DATA)),$(WISPS_DATA_DIRS)),)
$(error $(SANDPIT_DATA) is wisp's data directory; sandpit never writes there)
endif
endif

all: build netd initrd

build:
	$(GO) build -o bin/sandpitd ./cmd/sandpitd

netd:            ## root helper behind restrictive network policies; scripts/setup-host.sh installs it
	CGO_ENABLED=0 $(GO) build -o bin/sandpit-netd ./cmd/sandpit-netd

deps:            ## download firecracker + guest kernel into $(SANDPIT_DATA)
	./scripts/fetch-deps.sh

image:           ## build the base sprite disk image (rootless podman) into $(SANDPIT_DATA)
	@echo "image: writing $(SANDPIT_DATA)/images/base.ext4 (set DATA= to build elsewhere)"
	./scripts/build-image.sh

images:          ## firecracker + kernel, then every variant's disk: [DATA=<dir>] [VARIANTS='base e2b vercel daytona modal']
	./scripts/fetch-deps.sh
	@set -e; for v in $(VARIANTS); do echo "images: writing $(SANDPIT_DATA)/images/$$v.ext4"; ./scripts/build-image.sh $$v; done

initrd:          ## pack the guest agent into $(SANDPIT_DATA); takes effect on each sprite's next cold boot
	@echo "initrd: writing $(SANDPIT_DATA)/initrd.cpio (set DATA= to build elsewhere)"
	./scripts/build-initrd.sh

run: build initrd
	./bin/sandpitd --data $(SANDPIT_DATA)

# The one install path: sandpitd as systemd user service NAME (default sandpit)
# on DATA (default $(SANDPIT_DATA)). The checks run before the initrd is built
# into DATA, so a refused install writes nothing there. TAKEOVER=1 replaces a
# unit of the same NAME running wisp's sandboxd on the same DATA (see
# scripts/install-service.sh --takeover). FORCE_PAIR=1 lets the default name
# run on another data directory (a host whose data lives on its own volume), or
# another name on the default one (--force-pair).
INSTALL_ARGS = --name $(NAME) --data $(SANDPIT_DATA)$(if $(TAKEOVER), --takeover)$(if $(FORCE_PAIR), --force-pair)$(if $(FLAGS), -- $(FLAGS))
install-service: build   ## [NAME=sandpit] [DATA=<dir>] [FLAGS='--e2b-listen ...'] [TAKEOVER=1] [FORCE_PAIR=1]
	./scripts/install-service.sh --check $(INSTALL_ARGS)
	./scripts/build-initrd.sh
	./scripts/install-service.sh $(INSTALL_ARGS)

test: test-scripts
	$(GO) vet ./...
	$(GO) test -race -count=1 ./...

test-scripts:    ## install-service.sh against a throwaway HOME with stubbed systemctl
	./scripts/test-install-service.sh

# The daemon under test. Both default to the default install; set them (or
# SANDPIT_DATA / DATA=) to aim at a test stack, since the environment wins.
SPRITES_E2E_URL ?= http://127.0.0.1:7900
SPRITES_E2E_TOKEN ?= $(shell cat $(SANDPIT_DATA)/token 2>/dev/null)
e2e:             ## official Sprites Go SDK against a running sandpitd (SPRITES_E2E_URL, SPRITES_E2E_TOKEN)
	SPRITES_E2E_URL=$(SPRITES_E2E_URL) SPRITES_E2E_TOKEN=$(SPRITES_E2E_TOKEN) \
	  $(GO) test -tags e2e -count=1 -v ./e2e/
