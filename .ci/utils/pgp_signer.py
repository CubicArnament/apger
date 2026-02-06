"""
Модуль для PGP подписи пакетов
"""

import subprocess
import tempfile
from ..utils.logging_utils import get_logger


class PGPSigner:
    """
    Класс для подписи пакетов с использованием Sequoia PGP
    """
    
    def __init__(self):
        self.logger = get_logger(self.__class__.__name__)
    
    def sign_package_with_sq(self, package_path: str) -> bool:
        """
        Подписывает пакет с использованием sq (Sequoia PGP)
        """
        try:
            # Создаем detached подпись с помощью sq
            sign_result = subprocess.run([
                'sq', 'sign', '--detached', package_path
            ], capture_output=True, text=True)
            
            if sign_result.returncode != 0:
                self.logger.error(f"Ошибка создания PGP подписи с помощью sq: {sign_result.stderr}")
                return False
            
            self.logger.info(f"Пакет {package_path} успешно подписан с помощью sq")
            return True
            
        except FileNotFoundError:
            self.logger.error("Команда sq не найдена. Убедитесь, что установлен Sequoia PGP")
            return False
        except Exception as e:
            self.logger.error(f"Ошибка при подписании пакета: {e}")
            return False