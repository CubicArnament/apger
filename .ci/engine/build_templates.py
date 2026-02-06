"""
Модуль для шаблонов сборки
"""

import subprocess
import shutil
from pathlib import Path
from typing import Optional, List
import os
from ..utils.logging_utils import get_logger


class BuildTemplate:
    """
    Абстрактный класс для шаблонов сборки
    """
    
    def __init__(self, name: str, source_dir: str, build_dir: str, install_dir: str):
        self.name = name
        self.source_dir = source_dir
        self.build_dir = build_dir
        self.install_dir = install_dir
        self.logger = get_logger(self.__class__.__name__)
    
    def setup(self, extra_flags: Optional[List[str]] = None) -> bool:
        """Настройка перед сборкой"""
        raise NotImplementedError
    
    def compile(self) -> bool:
        """Компиляция проекта"""
        raise NotImplementedError
    
    def install(self) -> bool:
        """Установка в временный каталог"""
        raise NotImplementedError


class MesonTemplate(BuildTemplate):
    """
    Шаблон сборки для Meson
    """
    
    def setup(self, extra_flags: Optional[List[str]] = None) -> bool:
        """Настройка Meson проекта"""
        self.logger.info(f"[BUILD] Настройка Meson для {self.name}")
        
        cmd = [
            'meson', 'setup',
            self.build_dir,
            self.source_dir,
            f'--prefix=/usr',
            f'--destdir={self.install_dir}'
        ]
        
        if extra_flags:
            cmd.extend(extra_flags)
        
        try:
            result = subprocess.run(cmd, check=True, capture_output=True, text=True)
            self.logger.info(f"[BUILD] Настройка Meson завершена для {self.name}")
            return True
        except subprocess.CalledProcessError as e:
            self.logger.error(f"[BUILD] Ошибка настройки Meson для {self.name}: {e.stderr}")
            return False
    
    def compile(self) -> bool:
        """Компиляция Meson проекта"""
        self.logger.info(f"[BUILD] Компиляция Meson для {self.name}")
        
        cmd = ['meson', 'compile', '-C', self.build_dir]
        
        try:
            result = subprocess.run(cmd, check=True, capture_output=True, text=True)
            self.logger.info(f"[BUILD] Компиляция Meson завершена для {self.name}")
            return True
        except subprocess.CalledProcessError as e:
            self.logger.error(f"[BUILD] Ошибка компиляции Meson для {self.name}: {e.stderr}")
            return False
    
    def install(self) -> bool:
        """Установка Meson проекта"""
        self.logger.info(f"[BUILD] Установка Meson для {self.name}")
        
        cmd = ['meson', 'install', '-C', self.build_dir, '--skip-subprojects']
        
        try:
            result = subprocess.run(cmd, check=True, capture_output=True, text=True)
            self.logger.info(f"[BUILD] Установка Meson завершена для {self.name}")
            return True
        except subprocess.CalledProcessError as e:
            self.logger.error(f"[BUILD] Ошибка установки Meson для {self.name}: {e.stderr}")
            return False


class CMakeTemplate(BuildTemplate):
    """
    Шаблон сборки для CMake
    """
    
    def setup(self, extra_flags: Optional[List[str]] = None) -> bool:
        """Настройка CMake проекта"""
        self.logger.info(f"[BUILD] Настройка CMake для {self.name}")
        
        cmd = [
            'cmake',
            f'-S{self.source_dir}',
            f'-B{self.build_dir}',
            f'-DCMAKE_INSTALL_PREFIX=/usr',
            f'-DCMAKE_INSTALL_LIBDIR=lib',
            f'-DCMAKE_BUILD_TYPE=Release'
        ]
        
        if extra_flags:
            cmd.extend(extra_flags)
        
        try:
            result = subprocess.run(cmd, check=True, capture_output=True, text=True)
            self.logger.info(f"[BUILD] Настройка CMake завершена для {self.name}")
            return True
        except subprocess.CalledProcessError as e:
            self.logger.error(f"[BUILD] Ошибка настройки CMake для {self.name}: {e.stderr}")
            return False
    
    def compile(self) -> bool:
        """Компиляция CMake проекта"""
        self.logger.info(f"[BUILD] Компиляция CMake для {self.name}")
        
        cmd = ['cmake', '--build', self.build_dir, '--parallel']
        
        try:
            result = subprocess.run(cmd, check=True, capture_output=True, text=True)
            self.logger.info(f"[BUILD] Компиляция CMake завершена для {self.name}")
            return True
        except subprocess.CalledProcessError as e:
            self.logger.error(f"[BUILD] Ошибка компиляции CMake для {self.name}: {e.stderr}")
            return False
    
    def install(self) -> bool:
        """Установка CMake проекта"""
        self.logger.info(f"[BUILD] Установка CMake для {self.name}")
        
        cmd = ['cmake', '--install', self.build_dir, f'--prefix={self.install_dir}/usr']
        
        try:
            result = subprocess.run(cmd, check=True, capture_output=True, text=True)
            self.logger.info(f"[BUILD] Установка CMake завершена для {self.name}")
            return True
        except subprocess.CalledProcessError as e:
            self.logger.error(f"[BUILD] Ошибка установки CMake для {self.name}: {e.stderr}")
            return False


class AutotoolsTemplate(BuildTemplate):
    """
    Шаблон сборки для Autotools (configure/make/make install)
    """
    
    def setup(self, extra_flags: Optional[List[str]] = None) -> bool:
        """Настройка Autotools проекта"""
        self.logger.info(f"[BUILD] Настройка Autotools для {self.name}")
        
        configure_cmd = [
            f'{self.source_dir}/configure',
            f'--prefix=/usr',
            f'--libdir=/usr/lib',
            f'--disable-dependency-tracking',
            f'--disable-silent-rules'
        ]
        
        if extra_flags:
            configure_cmd.extend(extra_flags)
        
        try:
            result = subprocess.run(configure_cmd, cwd=self.source_dir, check=True, capture_output=True, text=True)
            self.logger.info(f"[BUILD] Настройка Autotools завершена для {self.name}")
            return True
        except subprocess.CalledProcessError as e:
            self.logger.error(f"[BUILD] Ошибка настройки Autotools для {self.name}: {e.stderr}")
            return False
    
    def compile(self) -> bool:
        """Компиляция Autotools проекта"""
        self.logger.info(f"[BUILD] Компиляция Autotools для {self.name}")
        
        cmd = ['make', '-j$(nproc)']
        
        try:
            result = subprocess.run(cmd, cwd=self.source_dir, check=True, capture_output=True, text=True)
            self.logger.info(f"[BUILD] Компиляция Autotools завершена для {self.name}")
            return True
        except subprocess.CalledProcessError as e:
            self.logger.error(f"[BUILD] Ошибка компиляции Autotools для {self.name}: {e.stderr}")
            return False
    
    def install(self) -> bool:
        """Установка Autotools проекта"""
        self.logger.info(f"[BUILD] Установка Autotools для {self.name}")
        
        cmd = ['make', f'DESTDIR={self.install_dir}', 'install']
        
        try:
            result = subprocess.run(cmd, cwd=self.source_dir, check=True, capture_output=True, text=True)
            self.logger.info(f"[BUILD] Установка Autotools завершена для {self.name}")
            return True
        except subprocess.CalledProcessError as e:
            self.logger.error(f"[BUILD] Ошибка установки Autotools для {self.name}: {e.stderr}")
            return False


class CargoTemplate(BuildTemplate):
    """
    Шаблон сборки для Cargo (Rust)
    """
    
    def setup(self, extra_flags: Optional[List[str]] = None) -> bool:
        """Настройка Cargo проекта"""
        self.logger.info(f"[BUILD] Настройка Cargo для {self.name}")
        
        # Для Rust обычно не требуется специальная настройка
        self.logger.info(f"[BUILD] Настройка Cargo не требуется для {self.name}")
        return True
    
    def compile(self) -> bool:
        """Компиляция Cargo проекта"""
        self.logger.info(f"[BUILD] Компиляция Cargo для {self.name}")
        
        cmd = ['cargo', 'build', '--release']
        
        if extra_flags:
            cmd.extend(extra_flags)
        
        try:
            result = subprocess.run(cmd, cwd=self.source_dir, check=True, capture_output=True, text=True)
            self.logger.info(f"[BUILD] Компиляция Cargo завершена для {self.name}")
            return True
        except subprocess.CalledProcessError as e:
            self.logger.error(f"[BUILD] Ошибка компиляции Cargo для {self.name}: {e.stderr}")
            return False
    
    def install(self) -> bool:
        """Установка Cargo проекта"""
        self.logger.info(f"[BUILD] Установка Cargo для {self.name}")
        
        # Для Rust нужно вручную скопировать бинарники
        target_dir = Path(self.source_dir) / 'target' / 'release'
        bin_dir = Path(self.install_dir) / 'usr' / 'bin'
        bin_dir.mkdir(parents=True, exist_ok=True)
        
        # Копируем все бинарники из target/release
        for binary in target_dir.iterdir():
            if binary.is_file() and not binary.name.endswith('.d'):
                dest_path = bin_dir / binary.name
                shutil.copy2(binary, dest_path)
        
        self.logger.info(f"[BUILD] Установка Cargo завершена для {self.name}")
        return True


class PythonPEP517Template(BuildTemplate):
    """
    Шаблон сборки для Python PEP 517
    """
    
    def setup(self, extra_flags: Optional[List[str]] = None) -> bool:
        """Настройка Python PEP 517 проекта"""
        self.logger.info(f"[BUILD] Настройка Python PEP 517 для {self.name}")
        
        # Устанавливаем зависимости для сборки
        try:
            subprocess.run(['pip', 'install', 'build', 'wheel'], check=True, capture_output=True, text=True)
            self.logger.info(f"[BUILD] Зависимости для Python PEP 517 установлены для {self.name}")
            return True
        except subprocess.CalledProcessError as e:
            self.logger.error(f"[BUILD] Ошибка установки зависимостей для Python PEP 517 для {self.name}: {e.stderr}")
            return False
    
    def compile(self) -> bool:
        """Сборка Python PEP 517 проекта"""
        self.logger.info(f"[BUILD] Сборка Python PEP 517 для {self.name}")
        
        cmd = ['python', '-m', 'build', '--wheel', '--outdir', self.build_dir]
        
        if extra_flags:
            # Добавляем флаги как параметры конфигурации
            for flag in extra_flags:
                if flag.startswith('--config-settings='):
                    cmd.append(flag)
        
        try:
            result = subprocess.run(cmd, cwd=self.source_dir, check=True, capture_output=True, text=True)
            self.logger.info(f"[BUILD] Сборка Python PEP 517 завершена для {self.name}")
            return True
        except subprocess.CalledProcessError as e:
            self.logger.error(f"[BUILD] Ошибка сборки Python PEP 517 для {self.name}: {e.stderr}")
            return False
    
    def install(self) -> bool:
        """Установка Python PEP 517 проекта"""
        self.logger.info(f"[BUILD] Установка Python PEP 517 для {self.name}")
        
        # Находим собранный wheel файл
        wheel_files = list(Path(self.build_dir).glob("*.whl"))
        
        if not wheel_files:
            self.logger.error(f"[BUILD] Не найдено wheel файлов для {self.name}")
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
            result = subprocess.run(cmd, check=True, capture_output=True, text=True)
            self.logger.info(f"[BUILD] Установка Python PEP 517 завершена для {self.name}")
            return True
        except subprocess.CalledProcessError as e:
            self.logger.error(f"[BUILD] Ошибка установки Python PEP 517 для {self.name}: {e.stderr}")
            return False


class BuildManager:
    """
    Менеджер для управления процессом сборки
    """
    
    def __init__(self):
        self.logger = get_logger(self.__class__.__name__)
    
    def get_template(self, template_name: str) -> Optional[type]:
        """Получение класса шаблона по имени"""
        # Используем match-case для определения шаблона
        match template_name:
            case 'meson':
                return MesonTemplate
            case 'cmake':
                return CMakeTemplate
            case 'autotools':
                return AutotoolsTemplate
            case 'cargo':
                return CargoTemplate
            case 'python-pep517':
                return PythonPEP517Template
            case _:
                return None
    
    def build_package(self, package_info: dict, source_dir: str, build_dir: str, install_dir: str) -> bool:
        """Выполнение сборки пакета"""
        build_info = package_info.get('build', {})
        template_name = build_info.get('template', 'meson')
        
        # Получаем класс шаблона
        template_class = self.get_template(template_name)
        if not template_class:
            self.logger.error(f"Шаблон сборки {template_name} не поддерживается")
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
            self.logger.error(f"Ошибка на этапе setup для {package_info['package']['name']}")
            return False
        
        if not builder.compile():
            self.logger.error(f"Ошибка на этапе compile для {package_info['package']['name']}")
            return False
        
        if not builder.install():
            self.logger.error(f"Ошибка на этапе install для {package_info['package']['name']}")
            return False
        
        self.logger.info(f"Сборка пакета {package_info['package']['name']} завершена успешно")
        return True