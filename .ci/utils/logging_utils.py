"""
@file logging_utils.py
@brief Модуль для логирования

Этот модуль предоставляет функции для настройки логирования
в приложении APGer Engine.
"""

import logging
import sys


def setup_logging(level=logging.INFO):
    """
    @brief Настраивает логирование для приложения
    @param level Уровень логирования
    @return Корневой логгер
    """
    # Создаем форматтер
    formatter = logging.Formatter(
        '%(asctime)s - %(name)s - %(levelname)s - %(message)s'
    )
    
    # Создаем хендлер для stdout
    handler = logging.StreamHandler(sys.stdout)
    handler.setFormatter(formatter)
    
    # Настраиваем корневой логгер
    root_logger = logging.getLogger()
    root_logger.setLevel(level)
    root_logger.addHandler(handler)
    
    return root_logger


def get_logger(name):
    """
    @brief Возвращает логгер с указанным именем
    @param name Имя логгера
    @return Логгер с указанным именем
    """
    return logging.getLogger(name)