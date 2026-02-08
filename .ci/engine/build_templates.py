"""
@file build_templates.py
@brief Модуль для шаблонов сборки

Этот модуль предоставляет классы шаблонов для различных систем сборки:
meson, cmake, autotools, cargo, python-pep517.
"""

import shutil
import subprocess
from pathlib import Path

from ..utils.logging_utils import get_logger


class BuildTemplate:
    """
    @class BuildTemplate
    @brief Абстрактный класс для шаблонов сборки

    Базовый класс для всех шаблонов сборки, определяющий интерфейс
    для настройки, компиляции и установки проектов.
    """

    def __init__(self, name: str, source_dir: str, build_dir: str, install_dir: str) -> None:
        """
        @brief Конструктор класса BuildTemplate
        @param name Имя пакета
        @param source_dir Директория с исходным кодом
        @param build_dir Директория для сборки
        @param install_dir Директория для установки
        """
        self.name = name
        self.source_dir = source_dir
        self.build_dir = build_dir
        self.install_dir = install_dir
        self.logger = get_logger(self.__class__.__name__)

    def setup(self, extra_flags: list[str] | None = None) -> bool:
        """
        @brief Настройка перед сборкой
        @param extra_flags Дополнительные флаги для настройки
        @return True в случае успеха, иначе False
        """
        raise NotImplementedError

    def compile(self) -> bool:
        """
        @brief Компиляция проекта
        @return True в случае успеха, иначе False
        """
        raise NotImplementedError

    def install(self) -> bool:
        """
        @brief Установка в временный каталог
        @return True в случае успеха, иначе False
        """
        raise NotImplementedError


class MesonTemplate(BuildTemplate):
    """
    @class MesonTemplate
    @brief Шаблон сборки для Meson

    Реализация шаблона сборки для системы сборки Meson.
    """

    def setup(self, extra_flags: list[str] | None = None) -> bool:
        """
        @brief Настройка Meson проекта
        @param extra_flags Дополнительные флаги для настройки
        @return True в случае успеха, иначе False
        """
        self.logger.info("[BUILD] Настройка Meson для %s", self.name)

        cmd = [
            'meson', 'setup',
            self.build_dir,
            self.source_dir,
            '--prefix=/usr',
            f"--destdir={self.install_dir}"
        ]

        if extra_flags:
            cmd.extend(extra_flags)

        try:
            subprocess.run(cmd, check=True, capture_output=True, text=True)
            self.logger.info("[BUILD] Настройка Meson завершена для %s", self.name)
            return True
        except subprocess.CalledProcessError as e:
            self.logger.exception("[BUILD] Ошибка настройки Meson для %s: %s", self.name, e.stderr)
            return False

    def compile(self) -> bool:
        """
        @brief Компиляция Meson проекта
        @return True в случае успеха, иначе False
        """
        self.logger.info("[BUILD] Компиляция Meson для %s", self.name)

        cmd = ['meson', 'compile', '-C', self.build_dir]

        try:
            subprocess.run(cmd, check=True, capture_output=True, text=True)
            self.logger.info("[BUILD] Компиляция Meson завершена для %s", self.name)
            return True
        except subprocess.CalledProcessError as e:
            self.logger.exception("[BUILD] Ошибка компиляции Meson для %s: %s", self.name, e.stderr)
            return False

    def install(self) -> bool:
        """
        @brief Установка Meson проекта
        @return True в случае успеха, иначе False
        """
        self.logger.info("[BUILD] Установка Meson для %s", self.name)

        cmd = ['meson', 'install', '-C', self.build_dir, '--skip-subprojects']

        try:
            subprocess.run(cmd, check=True, capture_output=True, text=True)
            self.logger.info("[BUILD] Установка Meson завершена для %s", self.name)
            return True
        except subprocess.CalledProcessError as e:
            self.logger.exception("[BUILD] Ошибка установки Meson для %s: %s", self.name, e.stderr)
            return False


class CMakeTemplate(BuildTemplate):
    """
    @class CMakeTemplate
    @brief Шаблон сборки для CMake

    Реализация шаблона сборки для системы сборки CMake.
    """

    def setup(self, extra_flags: list[str] | None = None) -> bool:
        """
        @brief Настройка CMake проекта
        @param extra_flags Дополнительные флаги для настройки
        @return True в случае успеха, иначе False
        """
        self.logger.info("[BUILD] Настройка CMake для %s", self.name)

        cmd = [
            'cmake',
            f"-S{self.source_dir}",
            f"-B{self.build_dir}",
            '-DCMAKE_INSTALL_PREFIX=/usr',
            '-DCMAKE_INSTALL_LIBDIR=lib',
            '-DCMAKE_BUILD_TYPE=Release'
        ]

        if extra_flags:
            cmd.extend(extra_flags)

        try:
            subprocess.run(cmd, check=True, capture_output=True, text=True)
            self.logger.info("[BUILD] Настройка CMake завершена для %s", self.name)
            return True
        except subprocess.CalledProcessError as e:
            self.logger.exception("[BUILD] Ошибка настройки CMake для %s: %s", self.name, e.stderr)
            return False

    def compile(self) -> bool:
        """
        @brief Компиляция CMake проекта
        @return True в случае успеха, иначе False
        """
        self.logger.info("[BUILD] Компиляция CMake для %s", self.name)

        cmd = ['cmake', '--build', self.build_dir, '--parallel']

        try:
            subprocess.run(cmd, check=True, capture_output=True, text=True)
            self.logger.info("[BUILD] Компиляция CMake завершена для %s", self.name)
            return True
        except subprocess.CalledProcessError as e:
            self.logger.exception("[BUILD] Ошибка компиляции CMake для %s: %s", self.name, e.stderr)
            return False

    def install(self) -> bool:
        """
        @brief Установка CMake проекта
        @return True в случае успеха, иначе False
        """
        self.logger.info("[BUILD] Установка CMake для %s", self.name)

        cmd = ['cmake', '--install', self.build_dir, f"--prefix={self.install_dir}/usr"]

        try:
            subprocess.run(cmd, check=True, capture_output=True, text=True)
            self.logger.info("[BUILD] Установка CMake завершена для %s", self.name)
            return True
        except subprocess.CalledProcessError as e:
            self.logger.exception("[BUILD] Ошибка установки CMake для %s: %s", self.name, e.stderr)
            return False


class AutotoolsTemplate(BuildTemplate):
    """
    @class AutotoolsTemplate
    @brief Шаблон сборки для Autotools (configure/make/make install)

    Реализация шаблона сборки для системы сборки Autotools.
    """

    def setup(self, extra_flags: list[str] | None = None) -> bool:
        """
        @brief Настройка Autotools проекта
        @param extra_flags Дополнительные флаги для настройки
        @return True в случае успеха, иначе False
        """
        self.logger.info("[BUILD] Настройка Autotools для %s", self.name)

        configure_cmd = [
            f"{self.source_dir}/configure",
            '--prefix=/usr',
            '--libdir=/usr/lib',
            '--disable-dependency-tracking',
            '--disable-silent-rules'
        ]

        if extra_flags:
            configure_cmd.extend(extra_flags)

        try:
            subprocess.run(configure_cmd, cwd=self.source_dir, check=True, capture_output=True, text=True)
            self.logger.info("[BUILD] Настройка Autotools завершена для %s", self.name)
            return True
        except subprocess.CalledProcessError as e:
            self.logger.exception("[BUILD] Ошибка настройки Autotools для %s: %s", self.name, e.stderr)
            return False

    def compile(self) -> bool:
        """
        @brief Компиляция Autotools проекта
        @return True в случае успеха, иначе False
        """
        self.logger.info("[BUILD] Компиляция Autotools для %s", self.name)

        cmd = ['make', '-j$(nproc)']

        try:
            subprocess.run(cmd, cwd=self.source_dir, check=True, capture_output=True, text=True)
            self.logger.info("[BUILD] Компиляция Autotools завершена для %s", self.name)
            return True
        except subprocess.CalledProcessError as e:
            self.logger.exception("[BUILD] Ошибка компиляции Autotools для %s: %s", self.name, e.stderr)
            return False

    def install(self) -> bool:
        """
        @brief Установка Autotools проекта
        @return True в случае успеха, иначе False
        """
        self.logger.info("[BUILD] Установка Autotools для %s", self.name)

        cmd = ['make', f"DESTDIR={self.install_dir}", 'install']

        try:
            subprocess.run(cmd, cwd=self.source_dir, check=True, capture_output=True, text=True)
            self.logger.info("[BUILD] Установка Autotools завершена для %s", self.name)
            return True
        except subprocess.CalledProcessError as e:
            self.logger.exception("[BUILD] Ошибка установки Autotools для %s: %s", self.name, e.stderr)
            return False


class CargoTemplate(BuildTemplate):
    """
    @class CargoTemplate
    @brief Шаблон сборки для Cargo (Rust)

    Реализация шаблона сборки для системы сборки Cargo (Rust).
    """

    def setup(self, extra_flags: list[str] | None = None) -> bool:
        """
        @brief Настройка Cargo проекта
        @param extra_flags Дополнительные флаги для настройки
        @return True в случае успеха, иначе False
        """
        self.logger.info("[BUILD] Настройка Cargo для %s", self.name)

        # Для Rust обычно не требуется специальная настройка
        self.logger.info("[BUILD] Настройка Cargo не требуется для %s", self.name)
        return True

    def compile(self, extra_flags: list[str] | None = None) -> bool:
        """
        @brief Компиляция Cargo проекта
        @param extra_flags Дополнительные флаги для компиляции
        @return True в случае успеха, иначе False
        """
        self.logger.info("[BUILD] Компиляция Cargo для %s", self.name)

        cmd = ['cargo', 'build', '--release']

        if extra_flags:
            cmd.extend(extra_flags)

        try:
            subprocess.run(cmd, cwd=self.source_dir, check=True, capture_output=True, text=True)
            self.logger.info("[BUILD] Компиляция Cargo завершена для %s", self.name)
            return True
        except subprocess.CalledProcessError as e:
            self.logger.exception("[BUILD] Ошибка компиляции Cargo для %s: %s", self.name, e.stderr)
            return False

    def install(self) -> bool:
        """
        @brief Установка Cargo проекта
        @return True в случае успеха, иначе False
        """
        self.logger.info("[BUILD] Установка Cargo для %s", self.name)

        # Для Rust нужно вручную скопировать бинарники
        target_dir = Path(self.source_dir) / 'target' / 'release'
        bin_dir = Path(self.install_dir) / 'usr' / 'bin'
        bin_dir.mkdir(parents=True, exist_ok=True)

        # Копируем все бинарники из target/release
        for binary in target_dir.iterdir():
            if binary.is_file() and not binary.name.endswith('.d'):
                dest_path = bin_dir / binary.name
                shutil.copy2(binary, dest_path)

        self.logger.info("[BUILD] Установка Cargo завершена для %s", self.name)
        return True


class GradleTemplate(BuildTemplate):
    """
    @class GradleTemplate
    @brief Шаблон сборки для Gradle (Java/Kotlin)

    Реализация шаблона сборки для системы сборки Gradle (Java/Kotlin).
    """

    def setup(self, extra_flags: list[str] | None = None) -> bool:
        """
        @brief Настройка Gradle проекта
        @param extra_flags Дополнительные фlags для настройки
        @return True в случае успеха, иначе False
        """
        self.logger.info("[BUILD] Настройка Gradle для %s", self.name)

        # Для Gradle обычно не требуется специальная настройка
        self.logger.info("[BUILD] Настройка Gradle не требуется для %s", self.name)
        return True

    def compile(self, extra_flags: list[str] | None = None) -> bool:
        """
        @brief Компиляция Gradle проекта
        @param extra_flags Дополнительные флаги для компиляции
        @return True в случае успеха, иначе False
        """
        self.logger.info("[BUILD] Компиляция Gradle для %s", self.name)

        cmd = ['./gradlew', 'build'] if (Path(self.source_dir) / 'gradlew').exists() else ['gradle', 'build']

        if extra_flags:
            cmd.extend(extra_flags)

        try:
            subprocess.run(cmd, cwd=self.source_dir, check=True, capture_output=True, text=True)
            self.logger.info("[BUILD] Компиляция Gradle завершена для %s", self.name)
            return True
        except subprocess.CalledProcessError as e:
            self.logger.exception("[BUILD] Ошибка компиляции Gradle для %s: %s", self.name, e.stderr)
            return False

    def install(self) -> bool:
        """
        @brief Установка Gradle проекта
        @return True в случае успеха, иначе False
        """
        self.logger.info("[BUILD] Установка Gradle для %s", self.name)

        # Находим JAR/WAR файлы и копируем их в директорию установки
        build_dir = Path(self.source_dir) / 'build' / 'libs'
        install_bin_dir = Path(self.install_dir) / 'usr' / 'share' / 'java'
        install_bin_dir.mkdir(parents=True, exist_ok=True)

        if build_dir.exists():
            for jar_file in build_dir.glob('*.jar'):
                dest_path = install_bin_dir / jar_file.name
                shutil.copy2(jar_file, dest_path)
            for war_file in build_dir.glob('*.war'):
                dest_path = install_bin_dir / war_file.name
                shutil.copy2(war_file, dest_path)

        self.logger.info("[BUILD] Установка Gradle завершена для %s", self.name)
        return True


class PythonPEP517Template(BuildTemplate):
    """
    @class PythonPEP517Template
    @brief Шаблон сборки для Python PEP 517

    Реализация шаблона сборки для системы сборки Python PEP 517.
    """

    def setup(self, extra_flags: list[str] | None = None) -> bool:
        """
        @brief Настройка Python PEP 517 проекта
        @param extra_flags Дополнительные флаги для настройки
        @return True в случае успеха, иначе False
        """
        self.logger.info("[BUILD] Настройка Python PEP 517 для %s", self.name)

        # Устанавливаем зависимости для сборки
        try:
            subprocess.run(['pip', 'install', 'build', 'wheel'], check=True, capture_output=True, text=True)
            self.logger.info("[BUILD] Зависимости для Python PEP 517 установлены для %s", self.name)
            return True
        except subprocess.CalledProcessError as e:
            self.logger.exception("[BUILD] Ошибка установки зависимостей для Python PEP 517 для %s: %s", self.name, e.stderr)
            return False

    def compile(self, extra_flags: list[str] | None = None) -> bool:
        """
        @brief Сборка Python PEP 517 проекта
        @param extra_flags Дополнительные флаги для компиляции
        @return True в случае успеха, иначе False
        """
        self.logger.info("[BUILD] Сборка Python PEP 517 для %s", self.name)

        cmd = ['python', '-m', 'build', '--wheel', '--outdir', self.build_dir]

        if extra_flags:
            # Добавляем флаги как параметры конфигурации
            for flag in extra_flags:
                if flag.startswith('--config-settings='):
                    cmd.append(flag)

        try:
            subprocess.run(cmd, cwd=self.source_dir, check=True, capture_output=True, text=True)
            self.logger.info("[BUILD] Сборка Python PEP 517 завершена для %s", self.name)
            return True
        except subprocess.CalledProcessError as e:
            self.logger.exception("[BUILD] Ошибка сборки Python PEP 517 для %s: %s", self.name, e.stderr)
            return False

    def install(self) -> bool:
        """
        @brief Установка Python PEP 517 проекта
        @return True в случае успеха, иначе False
        """
        self.logger.info("[BUILD] Установка Python PEP 517 для %s", self.name)

        # Находим собранный wheel файл
        wheel_files = list(Path(self.build_dir).glob("*.whl"))

        if not wheel_files:
            self.logger.error("[BUILD] Не найдено wheel файлов для %s", self.name)
            return False

        # Устанавливаем wheel в временный каталог
        wheel_file = wheel_files[0]
        cmd = [
            'pip', 'install',
            '--root', self.install_dir,
            '--no-deps',  # Не устанавливаем зависимости
            str(wheel_file)
        ]

        try:
            subprocess.run(cmd, check=True, capture_output=True, text=True)
            self.logger.info("[BUILD] Установка Python PEP 517 завершена для %s", self.name)
            return True
        except subprocess.CalledProcessError as e:
            self.logger.exception("[BUILD] Ошибка установки Python PEP 517 для %s: %s", self.name, e.stderr)
            return False


class BuildManager:
    """
    @class BuildManager
    @brief Менеджер для управления процессом сборки

    Класс, управляющий процессом сборки пакетов с использованием
    различных шаблонов сборки.
    """

    def __init__(self) -> None:
        """
        @brief Конструктор класса BuildManager
        """
        self.logger = get_logger(self.__class__.__name__)

    def get_template(self, template_name: str) -> type | None:
        """
        @brief Получение класса шаблона по имени
        @param template_name Имя шаблона сборки
        @return Класс шаблона или None если не найден
        """
        # Используем match-case для определения шаблона
        match template_name:
            case 'meson':
                self.logger.debug("Выбран шаблон Meson для сборки")
                return MesonTemplate
            case 'cmake':
                self.logger.debug("Выбран шаблон CMake для сборки")
                return CMakeTemplate
            case 'autotools':
                self.logger.debug("Выбран шаблон Autotools для сборки")
                return AutotoolsTemplate
            case 'cargo':
                self.logger.debug("Выбран шаблон Cargo для сборки")
                return CargoTemplate
            case 'python-pep517':
                self.logger.debug("Выбран шаблон Python PEP 517 для сборки")
                return PythonPEP517Template
            case 'gradle':
                self.logger.debug("Выбран шаблон Gradle для сборки")
                return GradleTemplate
            case _:
                self.logger.warning("Шаблон сборки %s не поддерживается", template_name)
                return None

    def build_package(self, package_info: dict, source_dir: str, build_dir: str, install_dir: str) -> bool:
        """
        @brief Выполнение сборки пакета
        @param package_info Информация о пакете
        @param source_dir Директория с исходным кодом
        @param build_dir Директория для сборки
        @param install_dir Директория для установки
        @return True в случае успеха, иначе False
        """
        build_info = package_info.get('build', {})
        template_name = build_info.get('template', 'meson')

        # Получаем класс шаблона
        template_class = self.get_template(template_name)
        if not template_class:
            self.logger.error("Шаблон сборки %s не поддерживается", template_name)
            return False

        # Создаем экземпляр шаблона
        builder = template_class(
            name=package_info['package']['name'],
            source_dir=source_dir,
            build_dir=build_dir,
            install_dir=install_dir
        )

        # Получаем дополнительные флаги
        extra_flags = build_info.get('extra_flags', [])
        if isinstance(extra_flags, str):
            extra_flags = [extra_flags]

        # Добавляем переведенные флаги, если они есть
        if 'translated_flags' in build_info:
            extra_flags.extend(build_info['translated_flags'])

        # Выполняем этапы сборки
        if not builder.setup(extra_flags):
            self.logger.error("Ошибка на этапе setup для %s", package_info['package']['name'])
            return False

        if not builder.compile():
            self.logger.error("Ошибка на этапе compile для %s", package_info['package']['name'])
            return False

        if not builder.install():
            self.logger.error("Ошибка на этапе install для %s", package_info['package']['name'])
            return False

        self.logger.info("Сборка пакета %s завершена успешно", package_info['package']['name'])
        return True
