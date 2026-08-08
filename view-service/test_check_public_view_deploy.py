import unittest

from check_public_view_deploy import validate


class PublicViewDeployPreflightTest(unittest.TestCase):
    def test_accepts_routing_subset_without_active_sub_filter(self):
        validate(
            {"zzc-tester-2642"},
            {"zzc-tester-2642", "zzc-t3vpilot2-2642"},
            "# sub_filter '/events' '/v2c/view/session/events';\nproxy_pass http://127.0.0.1:19324;",
        )

    def test_rejects_routing_session_missing_from_allowlist(self):
        with self.assertRaisesRegex(ValueError, "missing from nginx allowlist"):
            validate({"zzc-tester-2642", "zzc-new-2642"}, {"zzc-tester-2642"}, "")

    def test_rejects_active_sub_filter(self):
        with self.assertRaisesRegex(ValueError, "must not rewrite"):
            validate(
                {"zzc-tester-2642"},
                {"zzc-tester-2642"},
                "    sub_filter '/events' '/v2c/view/session/events';",
            )


if __name__ == "__main__":
    unittest.main()
