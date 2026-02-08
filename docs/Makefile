# Makefile для генерации HTML документации с помощью Doxygen

# Цель по умолчанию
help:
	@echo "Доступные цели:"
	@echo "  html_docs     - генерирует HTML документацию и открывает в браузере"
	@echo "  doxyfile_gen  - генерирует шаблон Doxyfile"
	@echo "  clean         - удаляет сгенерированную документацию"

# Генерация шаблона Doxyfile
doxyfile_gen:
	@echo "Генерация шаблона Doxyfile..."
	@doxygen -g Doxyfile
	@echo "Шаблон Doxyfile создан. Отредактируйте его в соответствии с вашими потребностями."

# Генерация HTML документации с помощью Doxygen
html_docs:
	@echo "Генерация HTML документации с помощью Doxygen..."
	@doxygen Doxyfile
	@echo "HTML документация сгенерирована в docs/html/index.html"
ifeq ($(OS),Windows_NT)
	start docs\html\index.html
else
	xdg-open docs/html/index.html 2>/dev/null || open docs/html/index.html 2>/dev/null || echo "Could not open browser"
endif

clean:
	@echo "Очистка сгенерированной документации..."
	-rmdir /s /q html 2>nul || rm -rf html
	@echo "Очистка завершена"