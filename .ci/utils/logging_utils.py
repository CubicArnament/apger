"""
@file logging_utils.py
@brief Модуль для логирования

Этот модуль предоставляет функции для настройки логирования
в приложении APGer Engine.
"""

import logging
import sys


def setup_logging(level=logging.INFO) -> logging.Logger:
    """
    @brief Configures logging for the application
    @param level Logging level (default INFO)
    @return Root logger
    """
    # Создаем форматтер
    formatter = logging.Formatter(
        "%(asctime)s - %(name)s - %(levelname)s - %(message)s"
    )

    # Создаем хендлер для stdout
    handler = logging.StreamHandler(sys.stdout)
    handler.setFormatter(formatter)

    # Настраиваем корневой логгер
    root_logger = logging.getLogger()

    # Используем match-case для определения уровня логирования
    match level:
        case logging.DEBUG:
            root_logger.setLevel(logging.DEBUG)
        case logging.INFO:
            root_logger.setLevel(logging.INFO)
        case logging.WARNING:
            root_logger.setLevel(logging.WARNING)
        case logging.ERROR:
            root_logger.setLevel(logging.ERROR)
        case logging.CRITICAL:
            root_logger.setLevel(logging.CRITICAL)
        case _:
            # По умолчанию используем INFO уровень
            root_logger.setLevel(logging.INFO)

    root_logger.addHandler(handler)

    return root_logger


def get_logger(name: str) -> logging.Logger:
    """
    @brief Returns a logger with the specified name
    @param name Logger name
    @return Logger with the specified name
    """
    return logging.getLogger(name)
