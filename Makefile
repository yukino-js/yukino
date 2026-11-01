.PHONY: commit
commit: config
	git add -A && git commit -m 'feat: Introduce new features'

.PHONY: feat
feat: commit
	git push origin main

.PHONY: config
config:
	git config user.name hangtiancheng && git config user.email '161043261@qq.com' 

BROWSER_DIST_SOURCE ?= $(HOME)/github/yukino-code/apps/yukino/src/remote/browser/dist
BROWSER_DIST_TARGET := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))internal/remote/browser/dist

.PHONY: sync-browser-dist
sync-browser-dist:
	@test -s "$(BROWSER_DIST_SOURCE)/index.html" || { echo "Browser build missing: $(BROWSER_DIST_SOURCE)/index.html" >&2; exit 1; }
	@mkdir -p "$(BROWSER_DIST_TARGET)"
	rsync -a --delete -- "$(BROWSER_DIST_SOURCE)/" "$(BROWSER_DIST_TARGET)/"
