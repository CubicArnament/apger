"""
@file version_resolver.py
@brief Модуль для динамического определения версии "latest"

Этот модуль предоставляет класс VersionResolver для определения
последней версии пакета из различных источников (GitHub, GitLab, и др.).
"""

import re
from urllib.parse import urlparse

import requests

from ..utils.logging_utils import get_logger


class VersionResolver:
    """
    @class VersionResolver
    @brief Класс для динамического определения версии "latest"

    Этот класс отвечает за определение последней версии пакета
    из различных источников (GitHub, GitLab, и др.).
    """

    def __init__(self) -> None:
        """
        @brief Конструктор класса VersionResolver
        """
        self.session = requests.Session()
        # Устанавливаем User-Agent для запросов к API
        self.session.headers.update({
            'User-Agent': 'APGer-Engine/1.0'
        })
        self.logger = get_logger(self.__class__.__name__)

    def resolve_latest_version(self, source_url: str) -> str:
        """
        @brief Определяет последнюю версию по URL источника
        @param source_url URL источника для проверки
        @return Последняя версия пакета
        """
        parsed_url = urlparse(source_url)

        # Используем match-case для определения провайдера
        match parsed_url.netloc:
            case host if 'github.com' in host:
                return self._resolve_github_latest(source_url)
            case host if 'gitlab.com' in host:
                return self._resolve_gitlab_latest(source_url)
            case host if 'kernel.org' in host or 'gnu.org' in host:
                # Дополнительный случай для kernel.org или gnu.org
                return self._resolve_generic_latest(source_url)
            case _:
                # Для других случаев пробуем получить родительскую директорию
                return self._resolve_generic_latest(source_url)

    def _resolve_github_latest(self, source_url: str) -> str:
        """
        @brief Определяет последнюю версию для GitHub репозитория
        @param source_url URL GitHub репозитория
        @return Последняя версия пакета
        """
        self.logger.info("[RESOLVE LATEST] Определение последней версии из GitHub...")

        # Извлекаем owner и repo из URL
        parsed_url = urlparse(source_url)
        path_parts = parsed_url.path.strip('/').split('/')

        # Поддерживаемые форматы GitHub URL:
        # - https://github.com/owner/repo/archive/refs/tags/v1.2.3.tar.gz
        # - https://github.com/owner/repo/releases/download/v1.2.3/tool-v1.2.3.tar.gz
        # - https://api.github.com/repos/owner/repo/releases/latest

        if len(path_parts) >= 2:
            owner = path_parts[0]
            repo = path_parts[1]

            # Получаем последний релиз через GitHub API
            api_url = f"https://api.github.com/repos/{owner}/{repo}/releases/latest"

            try:
                response = self.session.get(api_url)
                response.raise_for_status()

                release_data = response.json()
                tag_name = release_data.get('tag_name', '')

                # Удаляем префикс 'v' если он есть
                if tag_name.startswith('v'):
                    tag_name = tag_name[1:]

                self.logger.info(f"[RESOLVE LATEST] Найдена версия: {tag_name}")
                return tag_name

            except requests.RequestException as e:
                self.logger.exception(f"Ошибка при запросе к GitHub API: {e}")
                # Если API не сработал, пробуем другие методы

        # Если не удалось получить через API, пробуем другие подходы
        return self._resolve_generic_latest(source_url)

    def _resolve_gitlab_latest(self, source_url: str) -> str:
        """
        @brief Определяет последнюю версию для GitLab репозитория
        @param source_url URL GitLab репозитория
        @return Последняя версия пакета
        """
        self.logger.info("[RESOLVE LATEST] Определение последней версии из GitLab...")

        # Извлекаем owner и repo из URL
        parsed_url = urlparse(source_url)
        path_parts = parsed_url.path.strip('/').split('/')

        if len(path_parts) >= 2:
            # Для GitLab URL формата: https://gitlab.com/group/project/-/archive/v1.2.3/project-v1.2.3.tar.gz
            # или https://gitlab.com/api/v4/projects/group%2Fproject/repository/tags
            group_project = '/'.join(path_parts[:2])
            encoded_project = group_project.replace('/', '%2F')

            # Получаем последние теги через GitLab API
            api_url = f"https://gitlab.com/api/v4/projects/{encoded_project}/repository/tags"

            try:
                response = self.session.get(api_url)
                response.raise_for_status()

                tags = response.json()
                if tags:
                    # Берем первый тег (обычно они упорядочены по дате)
                    latest_tag = tags[0]['name']

                    # Удаляем префикс 'v' если он есть
                    if latest_tag.startswith('v'):
                        latest_tag = latest_tag[1:]

                    self.logger.info(f"[RESOLVE LATEST] Найдена версия: {latest_tag}")
                    return latest_tag

            except requests.RequestException as e:
                self.logger.exception(f"Ошибка при запросе к GitLab API: {e}")

        # Если не удалось получить через API, пробуем другие методы
        return self._resolve_generic_latest(source_url)

    def _resolve_generic_latest(self, source_url: str) -> str:
        """
        @brief Определяет последнюю версию для общего случая
        @param source_url URL источника для проверки
        @return Последняя версия пакета или "unknown"
        """
        self.logger.info("[RESOLVE LATEST] Определение последней версии из родительской директории...")

        # Пробуем получить список файлов в родительской директории
        parsed_url = urlparse(source_url)

        # Убираем имя файла из пути
        path_parts = parsed_url.path.rsplit('/', 1)[0]
        parent_dir_url = f"{parsed_url.scheme}://{parsed_url.netloc}{path_parts}"

        try:
            # Пытаемся получить страницу с родительской директорией
            response = self.session.get(parent_dir_url)
            response.raise_for_status()

            # Ищем возможные версии в HTML содержимом
            html_content = response.text

            # Простой паттерн для поиска версий в строках (например, v1.2.3 или 1.2.3)
            version_pattern = r'(?:^|[\s/_\-])(\d+\.\d+\.\d+)(?:[\s/_\-]|$)'
            matches = re.findall(version_pattern, html_content)

            match len(matches):
                case 0:
                    # Если ничего не нашли, возвращаем "unknown"
                    self.logger.warning("[RESOLVE LATEST] Не удалось определить последнюю версию, используем 'unknown'")
                    return "unknown"
                case _:
                    # Возвращаем последнюю найденную версию
                    latest_version = max(matches, key=lambda x: [int(i) for i in x.split('.')])
                    self.logger.info(f"[RESOLVE LATEST] Найдена версия: {latest_version}")
                    return latest_version

        except requests.RequestException as e:
            self.logger.exception(f"Не удалось получить родительскую директорию: {e}")
            # Если ничего не нашли, возвращаем "unknown"
            self.logger.warning("[RESOLVE LATEST] Не удалось определить последнюю версию, используем 'unknown'")
            return "unknown"
