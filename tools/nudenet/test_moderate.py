import contextlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import types
import unittest
from unittest.mock import Mock, patch

import moderate


class DetectionTests(unittest.TestCase):
    def test_default_classes_block_exposure(self):
        for label in moderate.DEFAULT_CLASSES:
            with self.subTest(label=label):
                self.assertTrue(moderate.assess_detections(
                    [{"class": label, "score": 0.6}], 0.6, moderate.DEFAULT_CLASSES,
                ))

    def test_below_threshold_and_other_classes_are_allowed(self):
        for detection in [
            {"class": "FEMALE_GENITALIA_EXPOSED", "score": 0.59},
            {"class": "FACE_MALE", "score": 0.99},
            {"class": "BELLY_EXPOSED", "score": 0.99},
            {"class": "MALE_BREAST_EXPOSED", "score": 0.99},
        ]:
            with self.subTest(detection=detection):
                self.assertEqual(moderate.assess_detections(
                    [detection], 0.6, moderate.DEFAULT_CLASSES,
                ), [])
        self.assertEqual(moderate.assess_detections([], 0.6, moderate.DEFAULT_CLASSES), [])

    def test_invalid_model_output_fails_closed(self):
        invalid = [
            None, {}, [None], [{}],
            [{"class": "UNKNOWN", "score": 0.9}],
            [{"class": [], "score": 0.9}],
        ]
        for score in [None, True, "0.9", float("nan"), float("inf"), -0.1, 1.1]:
            invalid.append([{"class": "FACE_MALE", "score": score}])
        for detections in invalid:
            with self.subTest(detections=detections):
                with self.assertRaises(ValueError):
                    moderate.assess_detections(detections, 0.6, moderate.DEFAULT_CLASSES)


class CommandCase(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.file_path = Path(self.directory.name) / "sample file.png"
        self.file_path.write_bytes(b"input")

    def run_command(self, arguments):
        stdout, stderr = io.StringIO(), io.StringIO()
        with contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
            status = moderate.main(arguments)
        return status, stdout.getvalue(), stderr.getvalue()


class CommandTests(CommandCase):
    def test_non_image_skips_nudenet(self):
        with patch.object(moderate, "inspect_image") as inspect:
            status, output, _ = self.run_command([str(self.file_path), "application/pdf"])
        self.assertEqual(status, 0)
        self.assertEqual(json.loads(output)["status"], "not_applicable")
        inspect.assert_not_called()

    def test_image_exit_codes_and_options(self):
        for result, want_status in [
            ({"status": "allowed", "frames_checked": 1}, 0),
            ({"status": "blocked", "frame": 0, "matches": []}, 1),
        ]:
            with self.subTest(result=result):
                with patch.object(moderate, "inspect_image", return_value=result) as inspect:
                    status, output, error = self.run_command([
                        "--threshold", "0.8", "--classes", "ANUS_EXPOSED",
                        str(self.file_path), "image/png",
                    ])
                self.assertEqual(status, want_status)
                self.assertEqual(json.loads(output), result)
                self.assertEqual(error, "")
                options = inspect.call_args.args[1]
                self.assertEqual(options.threshold, 0.8)
                self.assertEqual(options.classes, {"ANUS_EXPOSED"})

    def test_model_and_decode_errors_return_error_exit(self):
        for failure in [ImportError("NudeNet missing"), ValueError("invalid image"), RuntimeError("model unavailable")]:
            with self.subTest(failure=failure):
                with patch.object(moderate, "inspect_image", side_effect=failure):
                    status, output, error = self.run_command([str(self.file_path), "image/png"])
                self.assertEqual(status, 2)
                self.assertEqual(output, "")
                self.assertIn("NudeNet moderation failed:", error)

    def test_missing_input_returns_error_exit(self):
        status, output, error = self.run_command([str(self.file_path) + ".missing", "image/png"])
        self.assertEqual(status, 2)
        self.assertEqual(output, "")
        self.assertIn("regular file", error)

    def test_invalid_arguments_return_error_exit(self):
        for arguments in [
            [], [str(self.file_path)], ["--self-test", str(self.file_path)],
            ["--threshold", "nan"], ["--threshold", "0"], ["--threshold", "1.1"],
            ["--classes", "UNKNOWN"], ["--classes", ""], ["--max-frames", "0"],
            ["--max-pixels", "-1"],
        ]:
            with self.subTest(arguments=arguments):
                with contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit) as raised:
                    moderate.main(arguments)
                self.assertEqual(raised.exception.code, 2)

    def test_self_test_success_and_failure(self):
        with patch.object(moderate, "self_test", return_value={"status": "ready"}):
            self.assertEqual(self.run_command(["--self-test"])[0], 0)
        with patch.object(moderate, "self_test", side_effect=RuntimeError("model missing")):
            status, _, error = self.run_command(["--self-test"])
        self.assertEqual(status, 2)
        self.assertIn("model missing", error)


@unittest.skipUnless(importlib.util.find_spec("PIL") and importlib.util.find_spec("numpy"), "image dependencies not installed")
class ImageTests(CommandCase):
    def test_animated_image_checks_later_frames(self):
        from PIL import Image

        image_path = Path(self.directory.name) / "animated.gif"
        Image.new("RGB", (16, 16), "red").save(
            image_path, save_all=True, append_images=[Image.new("RGB", (16, 16), "blue")],
        )
        detector = Mock()
        detector.detect.side_effect = [[], [{"class": "ANUS_EXPOSED", "score": 0.95}]]
        with patch.dict("sys.modules", {"nudenet": types.SimpleNamespace(NudeDetector=lambda: detector)}):
            status, output, _ = self.run_command([str(image_path), "image/gif"])
        self.assertEqual(status, 1)
        self.assertEqual(json.loads(output)["frame"], 1)
        self.assertEqual(detector.detect.call_count, 2)

    def test_limits_and_corrupt_images_fail_closed(self):
        from PIL import Image

        Image.new("RGB", (16, 16), "red").save(self.file_path)
        detector = Mock()
        detector.detect.return_value = []
        for arguments in [
            ["--max-pixels", "100", str(self.file_path), "image/png"],
            [str(self.file_path), "image/svg+xml"],
        ]:
            with self.subTest(arguments=arguments):
                if arguments[-1] == "image/svg+xml":
                    self.file_path.write_bytes(b"<svg></svg>")
                with patch.dict("sys.modules", {"nudenet": types.SimpleNamespace(NudeDetector=lambda: detector)}):
                    status, _, error = self.run_command(arguments)
                self.assertEqual(status, 2)
                self.assertIn("NudeNet moderation failed:", error)
        detector.detect.assert_not_called()

    def test_too_many_frames_fail_closed(self):
        from PIL import Image

        image_path = Path(self.directory.name) / "animated.gif"
        Image.new("RGB", (16, 16), "red").save(
            image_path, save_all=True, append_images=[Image.new("RGB", (16, 16), "blue")],
        )
        with patch.dict("sys.modules", {"nudenet": types.SimpleNamespace(NudeDetector=Mock())}):
            status, _, error = self.run_command(["--max-frames", "1", str(image_path), "image/gif"])
        self.assertEqual(status, 2)
        self.assertIn("frame limit", error)


if __name__ == "__main__":
    unittest.main()
