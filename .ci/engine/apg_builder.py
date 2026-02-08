"""
@file apg_builder.py
@brief Module for creating APG packages in tar.zst format

This module provides the APGPackageBuilder class for creating
APG packages from installed files.
"""

import json
import shutil
import subprocess
import tarfile
import tempfile
import zlib
from pathlib import Path
from typing import Any

import yaml

from ..utils.logging_utils import get_logger


class APGPackageBuilder:
    """
    @class APGPackageBuilder
    @brief Class for creating APG packages in tar.zst format

    This class handles creating APG packages from installed files,
    including generating metadata, checksums, and packaging in an archive.
    """

    def __init__(self) -> None:
        """
        @brief Constructor of the APGPackageBuilder class
        """
        self.logger = get_logger(self.__class__.__name__)

    def create_apg_package(
        self,
        package_info: dict[str, Any],
        install_dir: str,
        output_dir: str,
        filesystem_yaml_path: str = "../.ci/filesystem_example.yaml",
        metadata_yaml_path: str = "../.ci/metadata_example.yaml"
    ) -> str:
        """
        @brief Creates an APG package from installed files
        @param package_info Information about the package
        @param install_dir Directory with installed files
        @param output_dir Output directory for the package
        @param filesystem_yaml_path Path to YAML file with filesystem configuration
        @param metadata_yaml_path Path to YAML file with metadata
        @return Path to the created APG package
        """
        package_name = package_info['package']['name']
        version = package_info['package']['version']
        arch = package_info['package']['architecture']

        # Создаем имя файла пакета
        filename = "{}-{}-{}.apg".format(package_name, version, arch)
        filepath = Path(output_dir) / filename

        # Создаем временную структуру пакета
        with tempfile.TemporaryDirectory() as temp_dir:
            temp_path = Path(temp_dir)

            # Копируем установленные файлы в data/
            data_dir = temp_path / 'data'
            if Path(install_dir).exists():
                shutil.copytree(install_dir, data_dir, dirs_exist_ok=True)

            # Загружаем конфигурацию файловой системы из YAML файла
            filesystem_config = self._load_filesystem_config(filesystem_yaml_path)

            # Применяем конфигурацию файловой системы
            if filesystem_config:
                self._apply_filesystem_config(data_dir, filesystem_config)

            # Загружаем метаданные из YAML файла
            metadata_config = self._load_metadata_config(metadata_yaml_path)

            # Объединяем метаданные из TOML и YAML
            combined_metadata = self._combine_metadata(package_info, metadata_config)

            # Создаем metadata.json
            metadata_path = temp_path / 'metadata.json'
            with open(metadata_path, 'w', encoding='utf-8') as f:
                json.dump(combined_metadata, f, indent=2, ensure_ascii=False)

            # Создаем контрольные суммы
            checksums_path = temp_path / 'crc32sums'
            self._generate_crc32_checksums(data_dir, checksums_path)

            # Создаем архив tar.zst
            self._create_tar_zst(temp_path, filepath)

        self.logger.info("APG пакет создан: %s", filepath)
        return str(filepath)

    def _load_metadata_config(self, yaml_path: str) -> dict[str, Any]:
        """
        Загружает конфигурацию метаданных из YAML файла
        """
        yaml_file = Path(yaml_path)
        if yaml_file.exists():
            with open(yaml_file, encoding='utf-8') as f:
                return yaml.safe_load(f)
        else:
            self.logger.warning("Файл конфигурации метаданных не найден: %s", yaml_path)
            return {}

    def _combine_metadata(self, json_metadata: dict[str, Any], yaml_metadata: dict[str, Any]) -> dict[str, Any]:
        """
        @brief Объединяет метаданные из JSON и YAML файлов
        @param json_metadata Метаданные из JSON файла
        @param yaml_metadata Метаданные из YAML файла
        @return Объединенные метаданные
        """
        # Начинаем с JSON метаданных
        combined = json_metadata.copy()

        # Обновляем или добавляем данные из YAML
        if 'package' in yaml_metadata:
            for key, value in yaml_metadata['package'].items():
                # Если ключ уже существует в JSON, объединяем списки или заменяем значение
                match key in combined['package']:
                    case True:
                        # Если значения являются списками, объединяем их
                        match isinstance(combined['package'][key], list) and isinstance(value, list):
                            case True:
                                combined['package'][key] = list(set(combined['package'][key] + value))  # Убираем дубликаты
                            case False:
                                # В противном случае, используем значение из JSON (приоритет)
                                pass
                    case False:
                        combined['package'][key] = value

        return combined

    def _load_filesystem_config(self, yaml_path: str) -> dict[str, Any]:
        """
        Загружает конфигурацию файловой системы из YAML файла
        """
        yaml_file = Path(yaml_path)
        if yaml_file.exists():
            with open(yaml_file, encoding='utf-8') as f:
                return yaml.safe_load(f)
        else:
            self.logger.warning("Файл конфигурации файловой системы не найден: %s", yaml_path)
            return {}

    def _apply_filesystem_config(self, data_dir: Path, filesystem_config: dict[str, Any]) -> None:
        """
        Применяет конфигурацию файловой системы к директории данных
        """
        # Создаем указанные директории
        if 'directories' in filesystem_config:
            for directory in filesystem_config['directories']:
                dir_path = data_dir / directory['path'].lstrip('/')
                dir_path.mkdir(parents=True, exist_ok=True)

                # Устанавливаем права доступа если указаны
                if 'permissions' in directory:
                    dir_path.chmod(int(directory['permissions'], 8))

        # Копируем указанные файлы
        if 'files' in filesystem_config:
            for file_entry in filesystem_config['files']:
                source_path = Path(file_entry['source'])
                dest_path = data_dir / file_entry['destination'].lstrip('/')

                # Создаем директорию назначения если не существует
                dest_path.parent.mkdir(parents=True, exist_ok=True)

                # Копируем файл
                if source_path.exists():
                    shutil.copy2(source_path, dest_path)

                    # Устанавливаем права доступа если указаны
                    if 'permissions' in file_entry:
                        dest_path.chmod(int(file_entry['permissions'], 8))

        # Создаем символические ссылки
        if 'symlinks' in filesystem_config:
            for symlink in filesystem_config['symlinks']:
                link_path = data_dir / symlink['destination'].lstrip('/')
                target_path = symlink['source']

                # Создаем директорию для ссылки если не существует
                link_path.parent.mkdir(parents=True, exist_ok=True)

                # Создаем символическую ссылку
                link_path.symlink_to(target_path)

    def _generate_metadata(self, package_info: dict[str, Any]) -> dict[str, Any]:
        """
        Генерирует метаданные для пакета
        """
        package = package_info['package']

        # Рассчитываем общий размер установленных файлов
        install_size = self._calculate_installed_size(Path(package_info.get('install_dir', '')))

        metadata = {
            'name': package['name'],
            'version': package['version'],
            'architecture': package['architecture'],
            'description': package.get('description', ''),
            'maintainer': package.get('maintainer', 'github-actions[bot]'),
            'license': package.get('license', ''),
            'homepage': package.get('homepage', ''),
            'source_url': package.get('source', ''),
            'installed_size': install_size,
            'sha256': '',  # Будет заполнен позже
            'dependencies': package.get('dependencies', []),
            'conflicts': package.get('conflicts', []),
            'provides': package.get('provides', []),
            'replaces': package.get('replaces', []),
            'conf': package.get('conf', []),
            'tags': package.get('tags', [])
        }

        return metadata

    def _calculate_installed_size(self, install_dir: Path) -> int:
        """
        Рассчитывает общий размер установленных файлов
        """
        total_size = 0

        if install_dir.exists():
            for file_path in install_dir.rglob('*'):
                if file_path.is_file():
                    total_size += file_path.stat().st_size

        return total_size

    def _generate_crc32_checksums(self, data_dir: Path, checksums_path: Path) -> None:
        """
        Генерирует CRC32 контрольные суммы для всех файлов
        """
        checksums = []

        if data_dir.exists():
            for file_path in data_dir.rglob('*'):
                if file_path.is_file():
                    # Читаем файл и вычисляем CRC32
                    with open(file_path, 'rb') as f:
                        content = f.read()
                        crc32_checksum = zlib.crc32(content) & 0xffffffff
                        # Получаем относительный путь от data_dir
                        rel_path = file_path.relative_to(data_dir)
                        checksums.append("{:08x}  {}".format(crc32_checksum, rel_path))

        # Записываем контрольные суммы в файл
        with open(checksums_path, 'w', encoding='utf-8') as f:
            f.write('\n'.join(checksums))

    def _create_tar_zst(self, source_dir: Path, output_path: Path) -> None:
        """
        Создает tar.zst архив
        """
        # Проверяем, установлен ли zstd
        try:
            subprocess.run(['which', 'zstd'], check=True, capture_output=True)
            subprocess.run(['zstd', '--version'], check=True, capture_output=True)
        except (subprocess.CalledProcessError, FileNotFoundError):
            self.logger.exception("Для создания APG пакетов необходим zstd")
            raise RuntimeError("Для создания APG пакетов необходим zstd") from None

        # Создаем tar архив
        tar_path = output_path.with_suffix('.tar')

        with tarfile.open(tar_path, 'w') as tar:
            for item in source_dir.iterdir():
                tar.add(item, arcname=item.name)

        # Сжимаем архив с помощью zstd
        subprocess.run([
            'zstd', '-19',  # Максимальное сжатие
            '-f',           # Перезаписать файл если существует
            str(tar_path)
        ], check=True, shell=False)

        # Переименовываем сжатый файл в .apg
        compressed_path = tar_path.with_suffix('.tar.zst')
        compressed_path.rename(output_path)
