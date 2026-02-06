"""
@file flag_translator.py
@brief Модуль для преобразования общих флагов в аргументы конкретных систем сборки

Этот модуль предоставляет класс FlagTranslator для преобразования
общих флагов в аргументы для конкретных систем сборки.
"""

from typing import Dict, List
from ..utils.logging_utils import get_logger


class FlagTranslator:
    """
    @class FlagTranslator
    @brief Класс для преобразования общих флагов в аргументы конкретных систем сборки
    
    Этот класс отвечает за преобразование общих флагов в аргументы
    для конкретных систем сборки (meson, cmake, autotools, и др.).
    """
    
    def __init__(self):
        """
        @brief Конструктор класса FlagTranslator
        """
        # Словари преобразования флагов для каждой системы сборки
        self.flag_mappings = {
            'meson': {
                'nls': '-Dnls=enabled',
                'shared': '-Ddefault_library=shared',
                'lto': '-Db_lto=true',
                'debug': '-Dbuildtype=debug',
                'release': '-Dbuildtype=release',
                'optimizations': '-Db_lto=true -Db_pgo=generate'
            },
            'cmake': {
                'nls': '-DENABLE_NLS=ON',
                'shared': '-DBUILD_SHARED_LIBS=ON',
                'lto': '-DCMAKE_INTERPROCEDURAL_OPTIMIZATION=ON',
                'debug': '-DCMAKE_BUILD_TYPE=Debug',
                'release': '-DCMAKE_BUILD_TYPE=Release',
                'optimizations': '-DCMAKE_INTERPROCEDURAL_OPTIMIZATION=ON'
            },
            'autotools': {
                'nls': '--enable-nls',
                'shared': '--enable-shared --disable-static',
                'lto': '--enable-lto',
                'debug': '--enable-debug',
                'release': '--disable-debug',
                'optimizations': '--enable-lto --enable-optimizations'
            },
            'cargo': {
                'nls': '--features=nls',
                'shared': '--features=shared',
                'lto': '--release',
                'debug': '--debug',
                'release': '--release',
                'optimizations': '--release'
            },
            'python-pep517': {
                'nls': '--config-settings="--build-option=--with-nls"',
                'shared': '--config-settings="--build-option=--enable-shared"',
                'lto': '--config-settings="--global-option=--enable-lto"',
                'debug': '--config-settings="--build-option=--debug"',
                'release': '--config-settings="--build-option=--release"',
                'optimizations': '--config-settings="--global-option=--enable-optimizations"'
            }
        }
        self.logger = get_logger(self.__class__.__name__)
    
    def translate_flags(self, build_system: str, use_flags: List[str]) -> List[str]:
        """
        @brief Преобразует общие флаги в аргументы для конкретной системы сборки
        @param build_system Система сборки
        @param use_flags Список флагов для преобразования
        @return Список преобразованных флагов
        """
        # Используем match-case для определения системы сборки
        match build_system:
            case system if system in self.flag_mappings:
                flag_map = self.flag_mappings[system]
                translated_flags = []
                
                for flag in use_flags:
                    match flag in flag_map:
                        case True:
                            translated_flags.append(flag_map[flag])
                        case False:
                            self.logger.warning(f"Флаг {flag} не поддерживается для системы сборки {system}")
                
                return translated_flags
            case _:
                self.logger.warning(f"Система сборки {build_system} не поддерживается в FlagTranslator")
                return []