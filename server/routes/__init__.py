"""EXAMVAN routes package.

Each sub-module registers its routes via @app.route decorators.
Importing this package triggers route registration.
"""
# Each module registers routes via @app.route when imported
from . import auth
from . import public_api
from . import admin_dashboard
from . import admin_exams
from . import admin_submissions
from . import admin_users
from . import admin_pengawas
from . import public_hasil
from . import downloads

# Re-export for backward compatibility (used by test files)
from ._shared import REQUIRED_ANDROID_VERSION
