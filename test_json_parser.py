#!/usr/bin/env python3
"""
Тестирование JSON-парсера
"""
import sys
from pathlib import Path

# Добавляем путь к .ci директории
ci_path = Path(__file__).parent / '.ci'
sys.path.insert(0, str(ci_path))

def test_json_parser():
    """Тестирование JSON-парсера"""
    try:
        from engine.json_parser import JSONParser
        parser = JSONParser('../repodata')  # Используем repodata как директорию
        packages = parser.parse_all_packages()
        print("Успешно распознано {} пакетов".format(len(packages)))
        
        if packages:
            pkg = packages[0]
            print("Пример данных пакета: {} - {}".format(pkg['package']['name'], pkg['package']['version']))
            
        return True
    except Exception as e:
        print("Ошибка при тестировании парсера: {}".format(e))
        return False

if __name__ == "__main__":
    test_json_parser()