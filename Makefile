.PHONY: fmt-check vet test test-cover test-race lint gosec docker-validate ansible-lint ansible-syntax repo-hygiene templates-validate actionlint docs-pdf ci-local

fmt-check:
	@files="$$(gofmt -l $$(git ls-files '*.go'))"; \
	if [ -n "$$files" ]; then \
		echo "Not gofmt-formatted:"; \
		echo "$$files"; \
		exit 1; \
	fi

vet:
	go vet ./...

test:
	go test ./...

test-cover:
	go test -covermode=atomic -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

test-race:
	go test -race ./...

lint:
	golangci-lint run --timeout=5m

gosec:
	gosec ./...

docker-validate:
	cp .env.example .env
	docker compose --env-file .env -f docker-compose.yml config -q
	docker compose --env-file .env -f docker-compose.monitoring.yml config -q
	docker compose --env-file .env -f deploy/compose/docker-compose.yml.tmpl config -q
	docker compose --env-file .env -f deploy/compose/docker-compose.monitoring.yml.tmpl config -q

ansible-lint:
	ANSIBLE_CONFIG=ansible/ansible.cfg ansible-lint -c .ansible-lint ansible

ansible-syntax:
	ANSIBLE_CONFIG=ansible/ansible.cfg ansible-playbook -i ansible/inventories/example/hosts.yml ansible/playbooks/bootstrap.yml --syntax-check
	ANSIBLE_CONFIG=ansible/ansible.cfg ansible-playbook -i ansible/inventories/example/hosts.yml ansible/playbooks/deploy.yml --syntax-check
	ANSIBLE_CONFIG=ansible/ansible.cfg ansible-playbook -i ansible/inventories/example/hosts.yml ansible/playbooks/security.yml --syntax-check

repo-hygiene:
	./scripts/repo_hygiene_check.sh

templates-validate:
	./scripts/validate_github_templates.sh

actionlint:
	go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12 -color

docs-pdf:
	./scripts/build_clients_pdf.sh

ci-local: repo-hygiene templates-validate fmt-check vet test docker-validate ansible-lint ansible-syntax
	@echo "Local CI checks passed"
