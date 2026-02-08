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
        print(f'Успешно распознано {len(packages)} пакетов')
        
        if packages:
            pkg = packages[0]
            print(f"Пример данных пакета: {pkg['package']['name']} - {pkg['package']['version']}")
            
        return True
    except Exception as e:
        print(f"Ошибка при тестировании парсера: {e}")
        return False

if __name__ == "__main__":
    test_json_parser()