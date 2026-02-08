# Makefile для генерации HTML документации с помощью Doxygen

# Цель по умолчанию
help:
	@echo "Доступные цели:"
	@echo "  html_docs     - генерирует HTML документацию"
	@echo "  open_docs     - открывает HTML документацию в браузере (локально)"
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

open_docs: html_docs
	@echo "Открытие HTML документации в браузере..."
ifeq ($(OS),Windows_NT)
	start html\index.html
else
	@xdg-open html/index.html 2>/dev/null || open html/index.html 2>/dev/null || echo "Could not open browser automatically. Please open html/index.html manually."
endif

gen_docs:
	@echo "Генерация HTML документации..."
	@doxygen Doxyfile
	@echo "Документация сгенерирована в html/index.html"

clean:
	@echo "Очистка сгенерированной документации..."
	-rmdir /s /q html 2>nul || rm -rf html
	@echo "Очистка завершена"