# Makefile для генерации HTML документации с помощью Doxygen

# Цель по умолчанию
help:
	@echo "Доступные цели:"
	@echo "  html_docs    - генерирует HTML документацию и открывает в браузере"
	@echo "  clean        - удаляет сгенерированную документацию"

# Генерация HTML документации с помощью Doxygen
html_docs:
	@echo "Генерация HTML документации с помощью Doxygen..."
	@echo "INPUT = .ci/engine .ci/utils" > Doxyfile
	@echo "OUTPUT_DIRECTORY = docs" >> Doxyfile
	@echo "PROJECT_NAME = \"APGer Engine\"" >> Doxyfile
	@echo "PROJECT_BRIEF = \"Flexible Python 3.12+ build engine for Arch Linux environments\"" >> Doxyfile
	@echo "GENERATE_HTML = YES" >> Doxyfile
	@echo "GENERATE_LATEX = NO" >> Doxyfile
	@echo "HTML_OUTPUT = html" >> Doxyfile
	@echo "FILE_PATTERNS = *.py" >> Doxyfile
	@echo "RECURSIVE = YES" >> Doxyfile
	@echo "EXTRACT_ALL = YES" >> Doxyfile
	@echo "EXTRACT_PRIVATE = NO" >> Doxyfile
	@echo "EXTRACT_STATIC = YES" >> Doxyfile
	@echo "SOURCE_BROWSER = YES" >> Doxyfile
	@echo "INLINE_SOURCES = NO" >> Doxyfile
	@echo "STRIP_CODE_COMMENTS = NO" >> Doxyfile
	@echo "ALPHABETICAL_INDEX = YES" >> Doxyfile
	@echo "COLS_IN_ALPHA_INDEX = 5" >> Doxyfile
	@echo "GENERATE_TREEVIEW = YES" >> Doxyfile
	@echo "DISABLE_INDEX = NO" >> Doxyfile
	@echo "GENERATE_TAGFILE = docs/html/APGer.tag" >> Doxyfile
	@echo "SEARCHENGINE = YES" >> Doxyfile
	@echo "SERVER_BASED_SEARCH = NO" >> Doxyfile
	@echo "EXTERNAL_SEARCH = NO" >> Doxyfile
	@echo "CLASS_DIAGRAMS = YES" >> Doxyfile
	@echo "HAVE_DOT = NO" >> Doxyfile
	@echo "DOT_FONTNAME = Helvetica" >> Doxyfile
	@echo "DOT_FONTSIZE = 10" >> Doxyfile
	@echo "UML_LOOK = NO" >> Doxyfile
	@echo "TEMPLATE_RELATIONS = NO" >> Doxyfile
	@echo "INCLUDE_GRAPH = NO" >> Doxyfile
	@echo "INCLUDED_BY_GRAPH = NO" >> Doxyfile
	@echo "CALL_GRAPH = NO" >> Doxyfile
	@echo "CALLER_GRAPH = NO" >> Doxyfile
	@echo "GRAPHICAL_HIERARCHY = YES" >> Doxyfile
	@echo "DIRECTORY_GRAPH = YES" >> Doxyfile
	@echo "DOT_IMAGE_FORMAT = png" >> Doxyfile
	@echo "INTERACTIVE_SVG = NO" >> Doxyfile
	@doxygen Doxyfile
	@echo "HTML документация сгенерирована в docs\html\index.html"
	start docs\html\index.html

clean:
	@echo "Очистка сгенерированной документации..."
	if exist docs\html rmdir /s /q docs\html
	if exist Doxyfile del /f Doxyfile
	@echo "Очистка завершена"