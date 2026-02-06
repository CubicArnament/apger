"""
Модуль для логирования
"""

import logging
import sys


def setup_logging(level=logging.INFO):
    """
    Настраивает логирование для приложения
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
    Возвращает логгер с указанным именем
    """
    return logging.getLogger(name)