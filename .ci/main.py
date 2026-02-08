#!/usr/bin/env python3
"""
@file main.py
@brief Главный исполняемый файл APGer Engine

Этот файл содержит основную логику для запуска APGer Engine,
включая парсинг TOML файлов, загрузку исходников, сборку пакетов
и создание APG архивов.
"""

import sys
import tempfile
from pathlib import Path

# Добавляем путь к модулям
sys.path.insert(0, str(Path(__file__).parent))

from engine.apg_builder import APGPackageBuilder
from engine.build_templates import BuildManager
from engine.json_parser import JSONParser
from engine.source_downloader import SourceDownloader
from utils.logging_utils import get_logger, setup_logging
from utils.pgp_signer import PGPSigner


def main() -> None:
    """
    @brief Main function to start APGer Engine
    """
    # Настраиваем логирование
    setup_logging()
    logger = get_logger(__name__)

    # Создаем экземпляры классов
    parser = JSONParser(repodata_dir="../repodata")  # Относительный путь к repodata
    downloader = SourceDownloader()
    builder = BuildManager()
    packager = APGPackageBuilder()
    signer = PGPSigner()

    # Парсим пакеты из repodata
    try:
        packages = parser.parse_all_packages()

        match len(packages):
            case 0:
                logger.info("Не найдено пакетов для обработки")
            case num_packages:
                logger.info(f"Найдено {num_packages} пакетов для обработки")
                for pkg in packages:
                    package_name = pkg['package']['name']
                    version = pkg['package']['version']

                    logger.info(f"Обработка пакета: {package_name} версии {version}")

                    # Создаем временные директории
                    with tempfile.TemporaryDirectory() as temp_dir:
                        temp_path = Path(temp_dir)
                        source_dir = temp_path / 'source'
                        build_dir = temp_path / 'build'
                        install_dir = temp_path / 'install'
                        output_dir = Path('dist/output')  # Выходная директория в dist/output
                        output_dir.mkdir(exist_ok=True)

                        # Загружаем исходники
                        source_url = pkg['package']['source']
                        match downloader.download_source(source_url, str(source_dir)):
                            case True:
                                logger.debug(f"Исходники успешно загружены для {package_name}")
                            case False:
                                logger.error(f"Ошибка загрузки исходников для {package_name}")
                                continue

                        # Выполняем сборку
                        match builder.build_package(pkg, str(source_dir), str(build_dir), str(install_dir)):
                            case True:
                                logger.debug(f"Пакет {package_name} успешно собран")
                            case False:
                                logger.error(f"Ошибка сборки пакета {package_name}")
                                continue

                        # Создаем APG пакет
                        package_path = packager.create_apg_package(pkg, str(install_dir), str(output_dir), "../.ci/filesystem_example.yaml", "../.ci/metadata_example.yaml")

                        # Подписываем пакет
                        match signer.sign_package_with_sq(package_path):
                            case True:
                                logger.info(f"Пакет {package_path} успешно подписан")
                            case False:
                                logger.error(f"Ошибка подписания пакета {package_path}")
                                continue

                    logger.info(f"Пакет {package_name} успешно обработан")
    except ValueError as e:
        logger.exception(f"Ошибка валидации repodata: {e}")
        return  # Останавливаем выполнение при ошибке валидации
    except Exception as e:
        logger.exception(f"Неожиданная ошибка при обработке repodata: {e}")
        return  # Останавливаем выполнение при любой ошибке


if __name__ == "__main__":
    main()
