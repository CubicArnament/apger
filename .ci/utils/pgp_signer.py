"""
@file pgp_signer.py
@brief Module for PGP signing of packages

This module provides the PGPSigner class for signing
packages using Sequoia PGP.
"""

import subprocess

from ..utils.logging_utils import get_logger


class PGPSigner:
    """
    @class PGPSigner
    @brief Class for signing packages using Sequoia PGP

    This class handles signing packages using
    Sequoia PGP (sq command).
    """

    def __init__(self) -> None:
        """
        @brief Constructor of the PGPSigner class
        """
        self.logger = get_logger(self.__class__.__name__)

    def sign_package_with_sq(self, package_path: str) -> bool:
        """
        @brief Signs a package using sq (Sequoia PGP)
        @param package_path Path to the package to sign
        @return True on success, otherwise False
        """
        try:
            # Создаем detached подпись с помощью sq
            sign_result = subprocess.run(
                ["sq", "sign", "--detached", package_path],
                capture_output=True,
                text=True,
            )

            # Используем match-case для обработки результата
            match sign_result.returncode:
                case 0:
                    self.logger.info(
                        f"Пакет {package_path} успешно подписан с помощью sq"
                    )
                    return True
                case _:
                    self.logger.error(
                        f"Ошибка создания PGP подписи с помощью sq: {sign_result.stderr}"
                    )
                    return False

        except FileNotFoundError:
            self.logger.exception(
                "Команда sq не найдена. Убедитесь, что установлен Sequoia PGP"
            )
            return False
        except Exception as e:
            self.logger.exception(f"Ошибка при подписании пакета: {e}")
            return False
