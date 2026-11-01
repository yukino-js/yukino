commit: config
	git add -A && git commit -m 'feat: Introduce new features'

push: commit
	git push

config:
	git config user.name hangtiancheng && git config user.email '161043261@qq.com' 
