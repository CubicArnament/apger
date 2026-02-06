"""
@file source_downloader.py
@brief Модуль для загрузки исходных кодов из различных источников

Этот модуль предоставляет класс SourceDownloader для загрузки
исходных кодов из различных источников (Git, архивы и др.).
"""

import requests
import subprocess
import tempfile
import os
import zipfile
import tarfile
from urllib.parse import urlparse
from pathlib import Path
from ..utils.logging_utils import get_logger


class SourceDownloader:
    """
    @class SourceDownloader
    @brief Класс для загрузки исходных кодов из различных источников
    
    Этот класс отвечает за загрузку исходных кодов из различных источников
    (Git, архивы и др.) и их подготовку для дальнейшей обработки.
    """
    
    def __init__(self):
        """
        @brief Конструктор класса SourceDownloader
        """
        self.session = requests.Session()
        self.session.headers.update({
            'User-Agent': 'APGer-Engine/1.0'
        })
        self.logger = get_logger(self.__class__.__name__)
    
    def download_source(self, source_url: str, destination_dir: str) -> bool:
        """
        @brief Загружает исходники из указанного URL в директорию назначения
        @param source_url URL источника
        @param destination_dir Директория назначения
        @return True в случае успеха, иначе False
        """
        self.logger.info(f"[DOWNLOAD] Загрузка исходников из {source_url}")
        
        parsed_url = urlparse(source_url)
        
        # Определяем тип источника
        if source_url.endswith('.git'):
            return self._download_git_repo(source_url, destination_dir)
        elif any(source_url.endswith(ext) for ext in ['.tar.gz', '.tar.xz', '.tar.bz2', '.zip']):
            return self._download_archive(source_url, destination_dir)
        else:
            # Для других URL пробуем как обычный архив
            return self._download_archive(source_url, destination_dir)
    
    def _download_git_repo(self, git_url: str, destination_dir: str) -> bool:
        """
        @brief Загружает Git репозиторий
        @param git_url URL Git репозитория
        @param destination_dir Директория для сохранения
        @return True в случае успеха, иначе False
        """
        try:
            result = subprocess.run([
                'git', 'clone', '--depth', '1', git_url, destination_dir
            ], check=True, capture_output=True, text=True)
            
            self.logger.info(f"[DOWNLOAD] Git репозиторий загружен в {destination_dir}")
            return True
        except subprocess.CalledProcessError as e:
            self.logger.error(f"[DOWNLOAD] Ошибка при клонировании Git репозитория: {e.stderr}")
            return False
    
    def _download_archive(self, archive_url: str, destination_dir: str) -> bool:
        """
        @brief Загружает и распаковывает архив
        @param archive_url URL архива
        @param destination_dir Директория для сохранения
        @return True в случае успеха, иначе False
        """
        # Создаем временный файл для загрузки
        with tempfile.NamedTemporaryFile(delete=False, suffix='.download') as tmp_file:
            try:
                # Загружаем архив
                response = self.session.get(archive_url, stream=True)
                response.raise_for_status()
                
                total_size = int(response.headers.get('content-length', 0))
                downloaded_size = 0
                
                with open(tmp_file.name, 'wb') as f:
                    for chunk in response.iter_content(chunk_size=8192):
                        if chunk:
                            f.write(chunk)
                            downloaded_size += len(chunk)
                            
                            if total_size > 0:
                                percent = (downloaded_size / total_size) * 100
                                print(f"\r[DOWNLOAD] Загрузка: {percent:.1f}%", end='', flush=True)
                
                print()  # Новая строка после прогресса
                
                # Распаковываем архив в зависимости от типа
                if archive_url.endswith('.zip'):
                    with zipfile.ZipFile(tmp_file.name, 'r') as zip_ref:
                        zip_ref.extractall(destination_dir)
                elif archive_url.endswith('.tar.gz') or archive_url.endswith('.tgz'):
                    with tarfile.open(tmp_file.name, 'r:gz') as tar_ref:
                        tar_ref.extractall(destination_dir)
                elif archive_url.endswith('.tar.xz'):
                    with tarfile.open(tmp_file.name, 'r:xz') as tar_ref:
                        tar_ref.extractall(destination_dir)
                elif archive_url.endswith('.tar.bz2'):
                    with tarfile.open(tmp_file.name, 'r:bz2') as tar_ref:
                        tar_ref.extractall(destination_dir)
                else:
                    # Пробуем определить тип архива автоматически
                    try:
                        with zipfile.ZipFile(tmp_file.name, 'r') as zip_ref:
                            zip_ref.extractall(destination_dir)
                    except zipfile.BadZipFile:
                        try:
                            with tarfile.open(tmp_file.name, 'r:*') as tar_ref:
                                tar_ref.extractall(destination_dir)
                        except tarfile.ReadError:
                            self.logger.error(f"[DOWNLOAD] Не удалось распознать формат архива: {archive_url}")
                            return False
                
                self.logger.info(f"[DOWNLOAD] Архив распакован в {destination_dir}")
                return True
                
            except requests.RequestException as e:
                self.logger.error(f"[DOWNLOAD] Ошибка при загрузке архива: {e}")
                return False
            except Exception as e:
                self.logger.error(f"[DOWNLOAD] Ошибка при распаковке архива: {e}")
                return False
            finally:
                # Удаляем временный файл
                os.unlink(tmp_file.name)