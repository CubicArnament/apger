"""
Модуль для парсинга TOML файлов
"""

import toml
from pathlib import Path
from typing import Dict, List, Any
from .version_resolver import VersionResolver
from .flag_translator import FlagTranslator


class TOMLParser:
    """
    Класс для парсинга TOML файлов из папки repodata
    """
    
    def __init__(self, repodata_dir: str = "repodata"):
        self.repodata_dir = Path(repodata_dir)
        self.version_resolver = VersionResolver()
        self.flag_translator = FlagTranslator()
        
    def parse_all_packages(self) -> List[Dict[str, Any]]:
        """
        Парсит все TOML файлы в папке repodata и возвращает список пакетов
        """
        packages = []
        
        match self.repodata_dir.exists():
            case True:
                toml_files = list(self.repodata_dir.glob("*.toml"))
                
                # Проверяем, есть ли TOML файлы в директории
                match len(toml_files):
                    case 0:
                        error_msg = f"Директория {self.repodata_dir} пуста - нет TOML файлов для обработки"
                        print(error_msg)
                        raise ValueError(error_msg)
                    case _:
                        for toml_file in toml_files:
                            try:
                                # Проверяем структуру TOML файла перед парсингом
                                if not self._validate_toml_structure(toml_file):
                                    error_msg = f"TOML файл {toml_file.name} не соответствует стандартной структуре"
                                    print(error_msg)
                                    raise ValueError(error_msg)
                                
                                package_data = self.parse_package(toml_file)
                                packages.append(package_data)
                                print(f"Парсинг {toml_file.name} завершен")
                            except Exception as e:
                                print(f"Ошибка при парсинге {toml_file.name}: {e}")
                                raise  # Перебрасываем ошибку, чтобы остановить сборку
            case False:
                error_msg = f"Директория {self.repodata_dir} не найдена"
                print(error_msg)
                raise ValueError(error_msg)
                
        return packages
    
    def _validate_toml_structure(self, toml_file: Path) -> bool:
        """
        Проверяет, соответствует ли TOML файл стандартной структуре
        """
        try:
            with open(toml_file, 'r', encoding='utf-8') as f:
                data = toml.load(f)
                
            # Проверяем наличие обязательных секций
            if 'package' not in data:
                print(f"Файл {toml_file.name} не содержит обязательной секции [package]")
                return False
                
            package_info = data['package']
            
            # Проверяем наличие обязательных полей в секции package
            required_fields = ['name', 'version', 'architecture', 'source']
            for field in required_fields:
                if field not in package_info:
                    print(f"Файл {toml_file.name} не содержит обязательного поля '{field}' в секции [package]")
                    return False
            
            # Проверяем, что поля имеют правильный тип
            if not isinstance(package_info['name'], str):
                print(f"Поле 'name' в файле {toml_file.name} должно быть строкой")
                return False
                
            if not isinstance(package_info['version'], str):
                print(f"Поле 'version' в файле {toml_file.name} должно быть строкой")
                return False
                
            if not isinstance(package_info['architecture'], str):
                print(f"Поле 'architecture' в файле {toml_file.name} должно быть строкой")
                return False
                
            if not isinstance(package_info['source'], str):
                print(f"Поле 'source' в файле {toml_file.name} должно быть строкой")
                return False
            
            # Проверяем наличие секции build
            if 'build' not in data:
                print(f"Файл {toml_file.name} не содержит обязательной секции [build]")
                return False
                
            build_info = data['build']
            
            # Проверяем наличие обязательного поля template в секции build
            if 'template' not in build_info:
                print(f"Файл {toml_file.name} не содержит обязательного поля 'template' в секции [build]")
                return False
                
            if not isinstance(build_info['template'], str):
                print(f"Поле 'template' в файле {toml_file.name} должно быть строкой")
                return False
            
            return True
            
        except Exception as e:
            print(f"Ошибка при проверке структуры TOML файла {toml_file.name}: {e}")
            return False
    
    def parse_package(self, toml_file: Path) -> Dict[str, Any]:
        """
        Парсит один TOML файл и возвращает словарь с данными пакета
        """
        with open(toml_file, 'r', encoding='utf-8') as f:
            data = toml.load(f)
            
        # Проверяем обязательные поля
        if 'package' not in data:
            raise ValueError(f"В файле {toml_file} отсутствует секция [package]")
            
        package_info = data['package']
        
        # Проверяем наличие обязательных полей
        required_fields = ['name', 'version', 'architecture', 'source']
        for field in required_fields:
            if field not in package_info:
                raise ValueError(f"В файле {toml_file} отсутствует поле {field} в секции [package]")
        
        # Если версия указана как "latest", определяем актуальную версию
        if package_info['version'] == 'latest':
            print(f"Определение последней версии для {package_info['name']}")
            resolved_version = self.version_resolver.resolve_latest_version(package_info['source'])
            package_info['version'] = resolved_version
        
        # Обрабатываем секцию build
        build_info = data.get('build', {})
        
        # Если указаны use флаги, преобразуем их в аргументы сборки
        if 'use' in build_info and 'template' in build_info:
            use_flags = build_info.get('use', [])
            build_template = build_info.get('template', 'meson')  # По умолчанию meson
            
            translated_flags = self.flag_translator.translate_flags(build_template, use_flags)
            build_info['translated_flags'] = translated_flags
        
        # Добавляем остальные секции если они есть
        result = {
            'package': package_info,
            'build': build_info,
        }
        
        return result