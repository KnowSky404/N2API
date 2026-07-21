from __future__ import annotations

import http.cookiejar
import json
import os
import secrets
import unittest
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass
from typing import Any, Callable, TypeVar

import openai
from openai import OpenAI


MODEL = "gpt-5"
REQUEST_TIMEOUT_SECONDS = 5.0
T = TypeVar("T")


class ContractFailure(AssertionError):
    def __init__(self, stage: str, detail: str) -> None:
        super().__init__(f"stage={stage} {detail}")


class AdminSession:
    def __init__(self, origin: str) -> None:
        self.origin = origin
        self.cookie_jar = http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(
            urllib.request.HTTPCookieProcessor(self.cookie_jar)
        )

    def request(
        self,
        method: str,
        path: str,
        stage: str,
        expected_status: int,
        body: dict[str, Any] | None = None,
        decode: bool = True,
    ) -> dict[str, Any] | None:
        encoded = None if body is None else json.dumps(body).encode("utf-8")
        headers = {"Accept": "application/json"}
        if encoded is not None:
            headers["Content-Type"] = "application/json"
        request = urllib.request.Request(
            f"{self.origin}{path}", data=encoded, headers=headers, method=method
        )
        try:
            response = self.opener.open(request, timeout=REQUEST_TIMEOUT_SECONDS)
        except urllib.error.HTTPError as error:
            try:
                error.read()
            except Exception:
                pass
            raise ContractFailure(
                stage, f"status={error.code} expected={expected_status}"
            ) from None
        except Exception:
            raise ContractFailure(stage, "failure=send_request") from None

        with response:
            status = response.status
            if status != expected_status:
                try:
                    response.read()
                except Exception:
                    pass
                raise ContractFailure(
                    stage, f"status={status} expected={expected_status}"
                )
            if not decode:
                try:
                    response.read()
                except Exception:
                    pass
                return None
            try:
                decoded = json.load(response)
            except Exception:
                raise ContractFailure(stage, "failure=decode_response") from None
        if not isinstance(decoded, dict):
            raise ContractFailure(stage, "field=response_type")
        return decoded

    def quiet(self, method: str, path: str, expected_status: int | None = None) -> None:
        try:
            expected = expected_status or (200 if method == "POST" else 204)
            self.request(method, path, "cleanup", expected, decode=False)
        except Exception:
            pass

    def clear(self) -> None:
        self.cookie_jar.clear()


@dataclass
class Resources:
    account_id: int = 0
    pool_id: int = 0
    key_id: int = 0
    client_secret: str = ""


class ContractFixture:
    def __init__(
        self,
        gateway_origin: str,
        admin_username: str,
        admin_password: str,
        mock_origin: str,
        mock_api_key: str,
    ) -> None:
        self.admin = AdminSession(gateway_origin)
        self.sdk_base_url = f"{gateway_origin}/v1"
        self.admin_username = admin_username
        self.admin_password = admin_password
        self.mock_origin = mock_origin
        self.mock_api_key = mock_api_key
        self.suffix = secrets.token_hex(8)
        self.resources = Resources()

    @classmethod
    def from_environment(cls) -> ContractFixture:
        required = (
            "N2API_CONTRACT_BASE_URL",
            "N2API_CONTRACT_ADMIN_USERNAME",
            "N2API_CONTRACT_ADMIN_PASSWORD",
            "N2API_CONTRACT_MOCK_BASE_URL",
            "N2API_CONTRACT_MOCK_API_KEY",
        )
        values: dict[str, str] = {}
        for name in required:
            value = os.environ.get(name, "").strip()
            if not value:
                raise ContractFailure("config", f"missing={name}")
            values[name] = value

        try:
            gateway = urllib.parse.urlsplit(values["N2API_CONTRACT_BASE_URL"])
            mock = urllib.parse.urlsplit(values["N2API_CONTRACT_MOCK_BASE_URL"])
        except Exception:
            raise ContractFailure("config", "field=url") from None
        if gateway.scheme != "http" or gateway.hostname != "n2api":
            raise ContractFailure("config", "field=gateway_host")
        if mock.scheme != "http" or mock.hostname != "mock-openai":
            raise ContractFailure("config", "field=mock_host")
        gateway_origin = f"{gateway.scheme}://{gateway.netloc}"
        mock_origin = f"{mock.scheme}://{mock.netloc}"
        return cls(
            gateway_origin,
            values["N2API_CONTRACT_ADMIN_USERNAME"],
            values["N2API_CONTRACT_ADMIN_PASSWORD"],
            mock_origin,
            values["N2API_CONTRACT_MOCK_API_KEY"],
        )

    def provision(self) -> None:
        self.admin.request(
            "POST",
            "/api/admin/login",
            "admin_login",
            200,
            {"username": self.admin_username, "password": self.admin_password},
        )

        account = self.admin.request(
            "POST",
            "/api/admin/provider-accounts/api-upstream",
            "account_create",
            201,
            {
                "name": f"SDK Python upstream {self.suffix}",
                "baseUrl": self.mock_origin,
                "apiKey": self.mock_api_key,
                "enabled": True,
                "priority": 0,
                "loadFactor": 1,
                "models": [MODEL],
            },
        )
        self.resources.account_id = int((account or {}).get("account", {}).get("id", 0))
        if self.resources.account_id <= 0:
            raise ContractFailure("account_create", "field=id")

        pool = self.admin.request(
            "POST",
            "/api/admin/routing-pools",
            "pool_create",
            201,
            {
                "name": f"sdk-python-{self.suffix}",
                "description": "OpenAI Python SDK contract",
                "enabled": True,
            },
        )
        self.resources.pool_id = int((pool or {}).get("pool", {}).get("id", 0))
        if self.resources.pool_id <= 0:
            raise ContractFailure("pool_create", "field=id")

        self.admin.request(
            "PUT",
            f"/api/admin/routing-pools/{self.resources.pool_id}/accounts",
            "pool_membership",
            200,
            {"accounts": [{"accountId": self.resources.account_id, "priority": 0}]},
        )

        key = self.admin.request(
            "POST",
            "/api/admin/keys",
            "client_key_create",
            201,
            {
                "name": f"sdk-python-{self.suffix}",
                "routingPoolId": self.resources.pool_id,
            },
        )
        self.resources.key_id = int((key or {}).get("key", {}).get("id", 0))
        self.resources.client_secret = str((key or {}).get("secret", ""))
        if self.resources.key_id <= 0 or not self.resources.client_secret:
            raise ContractFailure("client_key_create", "field=credentials")

    def cleanup(self) -> None:
        if self.resources.key_id > 0:
            self.admin.quiet(
                "POST", f"/api/admin/keys/{self.resources.key_id}/revoke"
            )
            self.admin.quiet("DELETE", f"/api/admin/keys/{self.resources.key_id}")
        if self.resources.pool_id > 0:
            self.admin.quiet(
                "DELETE", f"/api/admin/routing-pools/{self.resources.pool_id}"
            )
        if self.resources.account_id > 0:
            self.admin.quiet(
                "DELETE",
                f"/api/admin/provider-accounts/{self.resources.account_id}",
            )
        self.admin.quiet("POST", "/api/admin/logout", 204)
        self.resources.client_secret = ""
        self.admin.clear()


def sdk_stage(stage: str, action: Callable[[], T]) -> T:
    try:
        return action()
    except ContractFailure:
        raise
    except Exception:
        raise ContractFailure(stage, "failure=sdk_request") from None


class OpenAIContractTest(unittest.TestCase):
    def test_official_openai_python_sdk_matches_n2api_contract(self) -> None:
        fixture = ContractFixture.from_environment()
        succeeded = False
        try:
            fixture.provision()
            client = OpenAI(
                base_url=fixture.sdk_base_url,
                api_key=fixture.resources.client_secret,
                max_retries=0,
                timeout=REQUEST_TIMEOUT_SECONDS,
            )

            models = sdk_stage("models_list", client.models.list)
            self.assertTrue(
                any(model.id == MODEL for model in models.data),
                "stage=models_list field=model",
            )

            chat = sdk_stage(
                "chat_json",
                lambda: client.chat.completions.create(
                    model=MODEL,
                    messages=[
                        {"role": "user", "content": "Python SDK contract request"}
                    ],
                ),
            )
            self.assertEqual(
                chat.object, "chat.completion", "stage=chat_json field=object"
            )
            self.assertIsNotNone(chat.usage, "stage=chat_json field=usage")
            if chat.usage is None:
                raise ContractFailure("chat_json", "field=usage")
            self.assertEqual(
                chat.usage.total_tokens, 25, "stage=chat_json field=usage"
            )

            stream = sdk_stage(
                "responses_stream",
                lambda: client.responses.create(
                    model=MODEL,
                    input="Python SDK contract request",
                    stream=True,
                ),
            )

            def consume_stream() -> bool:
                completed = False
                for event in stream:
                    if event.type == "response.completed":
                        completed = True
                return completed

            self.assertTrue(
                sdk_stage("responses_stream", consume_stream),
                "stage=responses_stream field=completed",
            )

            invalid_client = OpenAI(
                base_url=fixture.sdk_base_url,
                api_key="n2api_invalid_contract_key",
                max_retries=0,
                timeout=REQUEST_TIMEOUT_SECONDS,
            )
            authentication_error = False
            try:
                invalid_client.models.list()
            except openai.AuthenticationError as error:
                authentication_error = error.status_code == 401
            except Exception:
                authentication_error = False
            self.assertTrue(
                authentication_error, "stage=invalid_key field=error_type"
            )
            succeeded = True
        finally:
            preserve_failure_state = (
                os.environ.get("N2API_CONTRACT_PRESERVE_FAILURE_STATE") == "true"
            )
            if succeeded or not preserve_failure_state:
                fixture.cleanup()


if __name__ == "__main__":
    unittest.main(verbosity=2)
