#!/usr/bin/env python3
"""
APGer Engine - Main executable
"""

import sys
import tempfile
from pathlib import Path

# Добавляем путь к модулям
sys.path.insert(0, str(Path(__file__).parent))

from engine.toml_parser import TOMLParser
from engine.source_downloader import SourceDownloader
from engine.build_templates import BuildManager
from engine.apg_builder import APGPackageBuilder
from utils.pgp_signer import PGPSigner
from utils.logging_utils import setup_logging, get_logger


def main():
    # Настраиваем логирование
    setup_logging()
    logger = get_logger(__name__)
    
    # Создаем экземпляры классов
    parser = TOMLParser(repodata_dir="../repodata")  # Относительный путь к repodata
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
            case _:
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
                        if not downloader.download_source(source_url, str(source_dir)):
                            logger.error(f"Ошибка загрузки исходников для {package_name}")
                            continue

                        # Выполняем сборку
                        if not builder.build_package(pkg, str(source_dir), str(build_dir), str(install_dir)):
                            logger.error(f"Ошибка сборки пакета {package_name}")
                            continue

                        # Создаем APG пакет
                        package_path = packager.create_apg_package(pkg, str(install_dir), str(output_dir), "../.ci/filesystem_example.yaml", "../.ci/metadata_example.yaml")

                        # Подписывание пакета будет происходить в GitHub Actions с помощью sq
                        logger.info(f"Пакет {package_path} создан, подпись будет выполнена в GitHub Actions")

                    logger.info(f"Пакет {package_name} успешно обработан")
    except ValueError as e:
        logger.error(f"Ошибка валидации repodata: {e}")
        return  # Останавливаем выполнение при ошибке валидации
    except Exception as e:
        logger.error(f"Неожиданная ошибка при обработке repodata: {e}")
        return  # Останавливаем выполнение при любой ошибке


if __name__ == "__main__":
    main()