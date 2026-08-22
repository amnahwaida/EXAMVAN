package com.examvan.app.helper

/**
 * Kontrak event presence access-log (fix temuan review ronde 3 #4).
 *
 * Server mencatat login/logout siswa via POST /access-log; auto-submit
 * kini juga dilaporkan dengan event eksplisit agar log presence membedakan
 * penutupan ujian normal vs otomatis (deadline / keluar app / overlay).
 */
object AccessLogPolicy {

    const val EVENT_LOGIN = "login"
    const val EVENT_LOGOUT = "logout"
    const val EVENT_AUTO_SUBMIT = "auto_submit"
}
