include Makefile.variables

ifeq ($(OS),Windows_NT)
detected_OS := windows
else
detected_OS := $(shell uname | tr [:upper:] [:lower:])
endif

ifeq ($(detected_OS),darwin)
sedopt := -i "" -e
else
sedopt := -i -e
endif

TARGET_VERSION ?= latest
# mkdocs.yml refuses to live in the docs-directory, this is why "cd .." is needed
# gives: "Error: The 'docs_dir' should not be the parent directory of the config file."
# even if docs_dir is "."
docs: .require-mkdocs
	cd .. && mkdocs build --strict

.PHONY: .require-mkdocs
.require-mkdocs:
	@which mkdocs >/dev/null 2>/dev/null || { \
	  echo 'mkdocs required, use pip install --disable-pip-version-check -r $(CURDIR)/requirements.txt' >&2; \
	  exit 1; \
	}

.docker-image.serve-dev.stamp: Dockerfile.serve-dev requirements_pip.txt requirements.txt Makefile.variables
	docker build \
	  --build-arg PYTHON_IMAGE_VERSION=$(python_version)-alpine$(alpine_version) \
	  --build-arg KUBE_VERSION=$(kubernetes_version) \
	  -t 'k0sdocs$(basename $@)' -f '$<' .
	touch -- '$@'

.PHONY: update-pip-requirements
update-pip-requirements: .docker-image.serve-dev.stamp
	docker run --rm --entrypoint sh k0sdocs.docker-image.serve-dev -c \
	'pip install --disable-pip-version-check pip-tools > /dev/null \
	  && echo pip | pip-compile --allow-unsafe --generate-hashes --output-file - - | grep -E -v "^ *#" \
	  && echo wheel | pip-compile --allow-unsafe --generate-hashes --output-file - - | grep -E -v "^ *#"' \
	  > requirements_pip.txt.tmp
	mv -- requirements_pip.txt.tmp requirements_pip.txt
