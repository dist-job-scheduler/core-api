# Buffer stress harness. Black-box over the public HTTP API; light by default.
#
#   make up        # build + start the SUT (postgres, migrate, server, scheduler, sink)
#   make tracer    # run the tracer-bullet spec
#   make down      # tear everything down (incl. volumes)
#
# Override numbers via env, e.g.:  ITEMS=50 RATE_LIMIT=20 make completeness

COMPOSE := docker compose -f docker-compose.stress.yml
K6_ENV  := $(if $(ITEMS),-e ITEMS=$(ITEMS),) $(if $(RATE_LIMIT),-e RATE_LIMIT=$(RATE_LIMIT),)

.PHONY: up down logs ps migrate tracer completeness rate retry crash k6-%

up:
	$(COMPOSE) build
	$(COMPOSE) run --rm migrate
	$(COMPOSE) up -d postgres sink server scheduler
	@echo "SUT up. server:8080  sink:9000"

down:
	$(COMPOSE) down -v

logs:
	$(COMPOSE) logs -f server scheduler sink

ps:
	$(COMPOSE) ps

# Run any spec by name: make k6-tracer, make k6-completeness, ...
k6-%:
	$(COMPOSE) run --rm $(K6_ENV) k6 run /scripts/$*.js

tracer:        ; $(MAKE) k6-tracer
completeness:  ; $(MAKE) k6-completeness
rate:          ; $(MAKE) k6-rate
retry:         ; $(MAKE) k6-retry

# Crash recovery is orchestrated (kills the scheduler mid-run), so it has its
# own script rather than a plain k6 invocation.
crash:
	./scripts/crash-run.sh
