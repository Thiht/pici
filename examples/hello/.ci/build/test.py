#!/usr/bin/env python3
import os

print("running tests...")
print("CI            =", os.environ.get("CI"))
print("PICI_WORKFLOW =", os.environ.get("PICI_WORKFLOW"))
print("PICI_COMMIT_SHA =", os.environ.get("PICI_COMMIT_SHA"))
