import argparse
import json
import math
from numbers import Real
from pathlib import Path
import sys
import warnings


LABELS = frozenset({
    "FEMALE_GENITALIA_COVERED", "FACE_FEMALE", "BUTTOCKS_EXPOSED",
    "FEMALE_BREAST_EXPOSED", "FEMALE_GENITALIA_EXPOSED", "MALE_BREAST_EXPOSED",
    "ANUS_EXPOSED", "FEET_EXPOSED", "BELLY_COVERED", "FEET_COVERED",
    "ARMPITS_COVERED", "ARMPITS_EXPOSED", "FACE_MALE", "BELLY_EXPOSED",
    "MALE_GENITALIA_EXPOSED", "ANUS_COVERED", "FEMALE_BREAST_COVERED",
    "BUTTOCKS_COVERED",
})
DEFAULT_CLASSES = frozenset({
    "FEMALE_GENITALIA_EXPOSED", "MALE_GENITALIA_EXPOSED", "ANUS_EXPOSED",
    "FEMALE_BREAST_EXPOSED", "BUTTOCKS_EXPOSED",
})


def probability(value):
    number = float(value)
    if not math.isfinite(number) or not 0 < number <= 1:
        raise argparse.ArgumentTypeError("threshold must be greater than 0 and at most 1")
    return number


def positive_integer(value):
    number = int(value)
    if number <= 0:
        raise argparse.ArgumentTypeError("limit must be a positive integer")
    return number


def blocking_classes(value):
    classes = frozenset(part.strip() for part in value.split(","))
    if not classes or not classes.issubset(LABELS):
        raise argparse.ArgumentTypeError("classes must be a non-empty list of NudeNet labels")
    return classes


def assess_detections(detections, threshold, classes):
    if not isinstance(detections, list):
        raise ValueError("detector returned an invalid detection list")
    matches = []
    for detection in detections:
        if not isinstance(detection, dict):
            raise ValueError("detector returned an invalid detection")
        label = detection.get("class")
        score = detection.get("score")
        if not isinstance(label, str) or label not in LABELS:
            raise ValueError("detector returned an unknown class")
        if (
            isinstance(score, bool)
            or not isinstance(score, Real)
            or not math.isfinite(score)
            or not 0 <= score <= 1
        ):
            raise ValueError("detector returned an invalid score")
        if label in classes and score >= threshold:
            matches.append({"class": label, "score": float(score)})
    return matches


def detect_frame(detector, frame, options):
    import numpy

    # Приводим кадр к формату BGR, который NudeNet получает при чтении через OpenCV
    rgb = frame.convert("RGB")
    rgb.thumbnail((1280, 1280))
    bgr = numpy.ascontiguousarray(numpy.asarray(rgb)[:, :, ::-1])
    return assess_detections(detector.detect(bgr), options.threshold, options.classes)


def inspect_image(file_path, options):
    from nudenet import NudeDetector
    from PIL import Image

    Image.MAX_IMAGE_PIXELS = options.max_pixels
    with warnings.catch_warnings():
        warnings.simplefilter("error", Image.DecompressionBombWarning)
        with Image.open(file_path) as image:
            frame_count = getattr(image, "n_frames", 1)
            if not 1 <= frame_count <= options.max_frames:
                raise ValueError("image frame limit exceeded")
            detector = NudeDetector()
            for frame_index in range(frame_count):
                image.seek(frame_index)
                if image.width * image.height > options.max_pixels:
                    raise ValueError("image pixel limit exceeded")
                matches = detect_frame(detector, image, options)
                if matches:
                    return {"status": "blocked", "frame": frame_index, "matches": matches}
    return {"status": "allowed", "frames_checked": frame_count}


def self_test(options):
    from importlib.metadata import version
    from nudenet import NudeDetector
    from PIL import Image

    if detect_frame(NudeDetector(), Image.new("RGB", (320, 320)), options):
        raise ValueError("model self-test unexpectedly blocked a blank image")
    return {"status": "ready", "model": "320n", "nudenet": version("nudenet")}


def main(arguments=None):
    parser = argparse.ArgumentParser(description="Local NudeNet adapter for Lyrarma Cloud")
    parser.add_argument("--threshold", type=probability, default=0.6)
    parser.add_argument("--classes", type=blocking_classes, default=DEFAULT_CLASSES)
    parser.add_argument("--max-frames", type=positive_integer, default=120)
    parser.add_argument("--max-pixels", type=positive_integer, default=25_000_000)
    parser.add_argument("--self-test", action="store_true")
    parser.add_argument("file_path", nargs="?")
    parser.add_argument("mime_type", nargs="?")
    options = parser.parse_args(arguments)
    if options.self_test:
        if options.file_path is not None or options.mime_type is not None:
            parser.error("--self-test cannot be combined with a file")
    elif options.file_path is None or options.mime_type is None:
        parser.error("file path and detected MIME type are required")

    try:
        if options.self_test:
            result = self_test(options)
        else:
            if not Path(options.file_path).is_file():
                raise ValueError("input is not a readable regular file")
            if options.mime_type.strip().lower().startswith("image/"):
                result = inspect_image(options.file_path, options)
            else:
                result = {"status": "not_applicable", "reason": "NudeNet only inspects images"}
        print(json.dumps(result, allow_nan=False))
        return 1 if result["status"] == "blocked" else 0
    except Exception as error:
        print(f"NudeNet moderation failed: {error}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
