"""EXAMVAN routes — Download pages and APK serving."""
import os

from flask import send_file, render_template

from app import (
    app,
    get_saas_setting, BASE_DIR,
)


@app.route('/download/apk')
def download_apk():
    """Download client Android APK."""
    apk_path = os.path.join(BASE_DIR, 'static', 'EXAMVAN.apk')
    return send_file(apk_path, as_attachment=True, download_name='EXAMVAN.apk')


@app.route('/download')
def download_page():
    """Render separate download page showing app and web versions."""
    android_ver = get_saas_setting('android_version', '2.1.0')
    webapp_ver = get_saas_setting('webapp_version', '2.1.0')
    apk_path = os.path.join(BASE_DIR, 'static', 'EXAMVAN.apk')
    file_size_mb = 0
    if os.path.exists(apk_path):
        file_size_mb = round(os.path.getsize(apk_path) / (1024 * 1024), 2)

    return render_template('download.html',
                           android_version=android_ver,
                           webapp_version=webapp_ver,
                           file_size_mb=file_size_mb)
