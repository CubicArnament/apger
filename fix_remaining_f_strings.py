import os
import re
from pathlib import Path

def fix_remaining_f_strings(file_path):
    with open(file_path, 'r', encoding='utf-8') as f:
        content = f.read()
    
    original_content = content
    
    # Паттерн для поиска всех f-строк (не только в логгерах)
    f_string_pattern = r'\bf([\'"])(.*?)\1'
    
    def replace_f_string(match):
        quote_char = match.group(1)
        f_string_content = match.group(2)
        
        # Найдем все переменные в фигурных скобках
        vars_in_braces = re.findall(r'\{([^}]+)\}', f_string_content)
        
        if not vars_in_braces:
            # Если нет переменных, просто убираем f
            return "{}{}{}".format(quote_char, f_string_content, quote_char)
        
        # Заменим {variable} на {}
        formatted_content = re.sub(r'\{([^}]+)\}', '{}', f_string_content)
        
        # Создадим строку с переменными
        vars_str = ', '.join(vars_in_braces)
        
        # Возвращаем форматированную строку с .format() методом
        return ""{}".format({})".format(formatted_content, vars_str)
    
    # Заменяем все f-строки
    updated_content = re.sub(f_string_pattern, replace_f_string, content)
    
    if updated_content != original_content:
        with open(file_path, 'w', encoding='utf-8') as f:
            f.write(updated_content)
        print("Обновлен файл: {}".format(file_path))
        return True
    return False

# Обработка всех Python файлов
root_path = Path('.')
updated_count = 0

for py_file in root_path.rglob('*.py'):
    if '__pycache__' not in str(py_file) and '.pyc' not in str(py_file) and '.git' not in str(py_file):
        try:
            if fix_remaining_f_strings(py_file):
                updated_count += 1
        except Exception as e:
            print("Ошибка при обработке {}: {}".format(py_file, e))

print("Обновлено файлов: {}".format(updated_count))