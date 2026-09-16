"""Regression checks for the private-to-public export boundary."""
import pathlib
import tempfile
import unittest
from unittest.mock import patch

import export_public as export


class PublicExportTests(unittest.TestCase):
    def test_private_paths_are_excluded(self):
        for path in (
            "manager/src/main.tsx", "backend/cmd/manager-api/main.go",
            "backend/internal/managerapi/plans.go",
            "backend/internal/db/migrations/00106_manager_admin.sql",
            "backend/db/queries/queries_manager.sql",
            "backend/internal/db/queries_manager.sql.go",
            "backend/cmd/worker/metering.go", "backend/cmd/worker/metering_test.go",
            "backend/db/queries/queries_usage.sql",
            "backend/internal/db/queries_usage.sql.go",
            "backend/internal/db/migrations/00102_tenant_daily_usage.sql",
            "scripts/public/edition_workers.go",
            "backend/internal/billing/stripe.go", "backend/internal/billing/vat.go",
            "backend/internal/httpapi/billing_handlers.go", "backend/internal/httpapi/signup.go",
            "frontend/src/pages/Signup.tsx", "frontend/src/panel/BillingSection.tsx",
            "frontend/src/panel/SubscribeWizard.tsx",
            "backend/internal/httpapi/waitlist_public.go", "backend/internal/httpapi/contact_public.go",
            "backend/internal/ses/ses.go", "backend/internal/provisioning/relay_test.go",
            "backend/internal/db/migrations/00140_waitlist.sql",
            "backend/internal/db/migrations/00107_mail_events.sql",
            "deploy/DIVERGENCES.md", "scripts/export_public.py",
            ".github/workflows/public-export.yml",
        ):
            with self.subTest(path=path):
                self.assertTrue(export.excluded(path))
        self.assertFalse(export.excluded("backend/db/queries/queries_plans.sql"))
        self.assertFalse(export.excluded("backend/cmd/worker/storage.go"))

    def test_cloud_metering_is_detected(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            for content in (
                "CREATE TABLE tenant_daily_usage", "type TenantDailyUsage struct {}",
                "func RollupTenantDailyUsage() {}", "func ExpiredRecordings() {}",
                "func DeleteRecordingRow() {}", "startMetering(ctx, q, st)",
            ):
                with self.subTest(content=content):
                    (root / "worker.go").write_text(content)
                    self.assertEqual(export.report_tree(root, manager_only=True), 1)

    def test_public_worker_hook_has_no_private_jobs(self):
        source = export.ROOT / export.OVERRIDES["backend/cmd/worker/edition_workers.go"]
        self.assertIn("func startEditionWorkers", source.read_text())
        self.assertNotIn("startMetering", source.read_text())

    def test_generated_manager_models_and_queries_are_detected(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            for content in (
                "type ManagerAudit struct {}", "func ManagerUsageSeries() {}",
                "SELECT * FROM manager_settings;",
            ):
                with self.subTest(content=content):
                    (root / "models.go").write_text(content)
                    report = export.residue(["models.go"], root)
                    self.assertEqual(report["manager console reference"], ["models.go (1)"])

    def test_overrides_and_generation_are_checked_at_destination(self):
        with tempfile.TemporaryDirectory() as source, tempfile.TemporaryDirectory() as output:
            root, target = pathlib.Path(source), pathlib.Path(output)
            (root / "private.md").write_text("manager-api")
            (root / "public.md").write_text("Public documentation")

            def generated(destination):
                (destination / "models.go").write_text("type ManagerSession struct {}")

            with patch.object(export, "ROOT", root), patch.object(
                export, "OVERRIDES", {"README.md": "public.md"}
            ), patch.object(export, "prepare_export", side_effect=generated):
                export.copy_tree([], target)
            self.assertEqual((target / "README.md").read_text(), "Public documentation")
            self.assertEqual(export.report_tree(target, manager_only=True), 1)

    def test_database_generation_removes_private_overrides(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            (root / "backend").mkdir()
            config = root / "backend/sqlc.yaml"
            config.write_text((export.ROOT / "backend/sqlc.yaml").read_text())
            with patch.object(export.subprocess, "run") as run:
                export.regenerate_database(root)
            run.assert_called_once_with(["sqlc", "generate"], cwd=root / "backend", check=True)
            self.assertNotIn("manager_", config.read_text())
            self.assertIn('column: "notifications.args"', config.read_text())

    def test_nonempty_destination_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            target = pathlib.Path(directory)
            (target / "existing").write_text("preserve")
            with self.assertRaises(SystemExit):
                export.copy_tree([], target)
            self.assertEqual((target / "existing").read_text(), "preserve")


if __name__ == "__main__":
    unittest.main()
