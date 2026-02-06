# Makefile для генерации HTML документации

# Цель по умолчанию
help:
	@echo "Доступные цели:"
	@echo "  html_docs    - генерирует HTML документацию и открывает в браузере"
	@echo "  clean        - удаляет сгенерированную документацию"

# Генерация HTML документации
html_docs:
	@echo "Генерация HTML документации..."
	@if not exist docs\html mkdir docs\html
	@echo ^<!DOCTYPE html^> > docs\html\index.html
	@echo ^<html^> >> docs\html\index.html
	@echo ^<head^> >> docs\html\index.html
	@echo   ^<title^>APGer Documentation^</title^> >> docs\html\index.html
	@echo   ^<style^> >> docs\html\index.html
	@echo     body { font-family: Arial, sans-serif; margin: 40px; } >> docs\html\index.html
	@echo     .nav { margin-top: 20px; } >> docs\html\index.html
	@echo     .section { margin-bottom: 40px; } >> docs\html\index.html
	@echo   ^</style^> >> docs\html\index.html
	@echo ^</head^> >> docs\html\index.html
	@echo ^<body^> >> docs\html\index.html
	@echo   ^<h1^>APGer Documentation^</h1^> >> docs\html\index.html
	@echo   ^<div class="section"^> >> docs\html\index.html
	@echo     ^<h2^>Overview^</h2^> >> docs\html\index.html
	@echo     ^<p^>APGer is a flexible Python 3.12+ build engine for creating APG packages for NurOS.^</p^> >> docs\html\index.html
	@echo     ^<p^>It automates the process of building packages from source code, generating metadata, and creating signed APG archives.^</p^> >> docs\html\index.html
	@echo   ^</div^> >> docs\html\index.html
	@echo   ^<div class="section"^> >> docs\html\index.html
	@echo     ^<h2^>Structure^</h2^> >> docs\html\index.html
	@echo     ^<ul^> >> docs\html\index.html
	@echo       ^<li^>repodata/: TOML files defining packages^</li^> >> docs\html\index.html
	@echo       ^<li^>.ci/: Build engine and configuration^</li^> >> docs\html\index.html
	@echo       ^<li^>.github/workflows/: CI/CD workflows^</li^> >> docs\html\index.html
	@echo     ^</ul^> >> docs\html\index.html
	@echo   ^</div^> >> docs\html\index.html
	@echo   ^<div class="section"^> >> docs\html\index.html
	@echo     ^<h2^>TOML Recipe Format^</h2^> >> docs\html\index.html
	@echo     ^<pre^> >> docs\html\index.html
	@echo [package] >> docs\html\index.html
	@echo name = "package-name" >> docs\html\index.html
	@echo version = "latest" # Must be dynamically resolved >> docs\html\index.html
	@echo architecture = "x86_64" >> docs\html\index.html
	@echo description = "Package description" >> docs\html\index.html
	@echo source = "https://example.com/source.tar.gz" >> docs\html\index.html
	@echo >> docs\html\index.html
	@echo [build] >> docs\html\index.html
	@echo template = "meson" # Options: cmake, meson, autotools, cargo, python-pep517 >> docs\html\index.html
	@echo use = ["shared", "lto", "nls"] >> docs\html\index.html
	@echo extra_flags = "-Dcustom_option=true" >> docs\html\index.html
	@echo     ^</pre^> >> docs\html\index.html
	@echo   ^</div^> >> docs\html\index.html
	@echo   ^<div class="section"^> >> docs\html\index.html
	@echo     ^<h2^>Build Process^</h2^> >> docs\html\index.html
	@echo     ^<ol^> >> docs\html\index.html
	@echo       ^<li^>Parse TOML recipes from repodata/^</li^> >> docs\html\index.html
	@echo       ^<li^>Download source code^</li^> >> docs\html\index.html
	@echo       ^<li^>Build using specified template^</li^> >> docs\html\index.html
	@echo       ^<li^>Create APG package^</li^> >> docs\html\index.html
	@echo       ^<li^>Sign package with PGP^</li^> >> docs\html\index.html
	@echo     ^</ol^> >> docs\html\index.html
	@echo   ^</div^> >> docs\html\index.html
	@echo   ^<div class="nav"^> >> docs\html\index.html
	@echo     ^<a href="#"^>Top^</a^> >> docs\html\index.html
	@echo   ^</div^> >> docs\html\index.html
	@echo ^</body^> >> docs\html\index.html
	@echo ^</html^> >> docs\html\index.html
	@echo "HTML документация сгенерирована в docs\html\index.html"
	start docs\html\index.html

clean:
	@echo "Очистка сгенерированной документации..."
	if exist docs\html rmdir /s /q docs\html
	@echo "Очистка завершена"