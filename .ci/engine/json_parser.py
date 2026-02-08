"""
@file json_parser.py
@brief Модуль для парсинга JSON файлов

Этот модуль предоставляет класс JSONParser для парсинга JSON файлов
из директории repodata и валидации их структуры.
"""

import json
from pathlib import Path
from typing import Any

from .flag_translator import FlagTranslator
from .version_resolver import VersionResolver


class JSONParser:
    """
    @class JSONParser
    @brief Класс для парсинга JSON файлов из папки repodata

    Этот класс отвечает за парсинг JSON файлов, валидацию их структуры
    и подготовку данных для дальнейшей обработки.
    """

    def __init__(self, repodata_dir: str = "repodata") -> None:
        """
        @brief Конструктор класса JSONParser
        @param repodata_dir Директория с JSON файлами (по умолчанию "repodata")
        """
        self.repodata_dir = Path(repodata_dir)
        self.version_resolver = VersionResolver()
        self.flag_translator = FlagTranslator()

    def parse_all_packages(self) -> list[dict[str, Any]]:
        """
        @brief Парсит все JSON файлы в папке repodata и возвращает список пакетов
        @return Список словарей с информацией о пакетах
        """
        packages = []

        match self.repodata_dir.exists():
            case True:
                json_files = list(self.repodata_dir.glob("*.json"))

                # Проверяем, есть ли JSON файлы в директории
                match len(json_files):
                    case 0:
                        error_msg = f"Директория {self.repodata_dir} пуста - нет JSON файлов для обработки"
                        print(error_msg)
                        raise ValueError(error_msg)
                    case _:
                        for json_file in json_files:
                            try:
                                # Проверяем структуру JSON файла перед парсингом
                                if not self._validate_json_structure(json_file):
                                    error_msg = f"JSON файл {json_file.name} не соответствует стандартной структуре"
                                    print(error_msg)
                                    raise ValueError(error_msg)

                                package_data = self.parse_package(json_file)
                                packages.append(package_data)
                                print(f"Парсинг {json_file.name} завершен")
                            except Exception as e:
                                print(f"Ошибка при парсинге {json_file.name}: {e}")
                                raise  # Перебрасываем ошибку, чтобы остановить сборку
            case False:
                error_msg = f"Директория {self.repodata_dir} не найдена"
                print(error_msg)
                raise ValueError(error_msg)

        return packages

    def _validate_json_structure(self, json_file: Path) -> bool:
        """
        @brief Проверяет, соответствует ли JSON файл стандартной структуре
        @param json_file Путь к JSON файлу для проверки
        @return True если структура корректна, иначе False
        """
        try:
            with open(json_file, encoding='utf-8') as f:
                data = json.load(f)

            # Проверяем наличие обязательных секций
            if 'package' not in data:
                print(f"Файл {json_file.name} не содержит обязательной секции 'package'")
                return False

            package_info = data['package']

            # Проверяем наличие обязательных полей в секции package
            required_fields = ['name', 'version', 'architecture', 'source']
            for field in required_fields:
                if field not in package_info:
                    print(f"Файл {json_file.name} не содержит обязательного поля '{field}' в секции 'package'")
                    return False

            # Проверяем, что поля имеют правильный тип
            match isinstance(package_info['name'], str):
                case False:
                    print(f"Поле 'name' в файле {json_file.name} должно быть строкой")
                    return False

            match isinstance(package_info['version'], str):
                case False:
                    print(f"Поле 'version' в файле {json_file.name} должно быть строкой")
                    return False

            match isinstance(package_info['architecture'], str):
                case False:
                    print(f"Поле 'architecture' в файле {json_file.name} должно быть строкой")
                    return False

            match isinstance(package_info['source'], str):
                case False:
                    print(f"Поле 'source' в файле {json_file.name} должно быть строкой")
                    return False

            # Проверяем наличие секции build
            if 'build' not in data:
                print(f"Файл {json_file.name} не содержит обязательной секции 'build'")
                return False

            build_info = data['build']

            # Проверяем наличие обязательного поля template в секции build
            if 'template' not in build_info:
                print(f"Файл {json_file.name} не содержит обязательного поля 'template' в секции 'build'")
                return False

            match isinstance(build_info['template'], str):
                case False:
                    print(f"Поле 'template' в файле {json_file.name} должно быть строкой")
                    return False

            # Проверяем, что build.dependencies является массивом строк (если присутствует)
            if 'dependencies' in build_info:
                match isinstance(build_info['dependencies'], list):
                    case False:
                        print(f"Поле 'dependencies' в секции 'build' файла {json_file.name} должно быть массивом")
                        return False
                    case True:
                        for dep in build_info['dependencies']:
                            match isinstance(dep, str):
                                case False:
                                    print(f"Каждая зависимость в 'build.dependencies' файла {json_file.name} должна быть строкой")
                                    return False

            return True

        except json.JSONDecodeError as e:
            print(f"Ошибка синтаксиса JSON в файле {json_file.name}: {e}")
            return False
        except Exception as e:
            print(f"Ошибка при проверке структуры JSON файла {json_file.name}: {e}")
            return False

    def parse_package(self, json_file: Path) -> dict[str, Any]:
        """
        @brief Парсит один JSON файл и возвращает словарь с данными пакета
        @param json_file Путь к JSON файлу
        @return Словарь с информацией о пакете
        """
        with open(json_file, encoding='utf-8') as f:
            data = json.load(f)

        # Проверяем обязательные поля
        if 'package' not in data:
            raise ValueError(f"В файле {json_file} отсутствует секция 'package'")

        package_info = data['package']

        # Проверяем наличие обязательных полей
        required_fields = ['name', 'version', 'architecture', 'source']
        for field in required_fields:
            if field not in package_info:
                raise ValueError(f"В файле {json_file} отсутствует поле {field} в секции 'package'")

        # Если версия указана как "latest", определяем актуальную версию
        match package_info['version']:
            case 'latest':
                print(f"Определение последней версии для {package_info['name']}")
                resolved_version = self.version_resolver.resolve_latest_version(package_info['source'])
                package_info['version'] = resolved_version
            case _:
                # Версия указана явно, ничего не меняем
                pass

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
