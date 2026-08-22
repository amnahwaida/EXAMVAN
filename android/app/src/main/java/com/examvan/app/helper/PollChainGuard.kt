package com.examvan.app.helper

/**
 * Guard generasi rantai poll konfirmasi pinning (fix temuan review strict
 * #1: "setiap retry aktivasi membuat rantai poll baru tanpa membatalkan
 * rantai lama — callback onResult ganda, entri audit duplikat, balapan
 * hasil antar rantai").
 *
 * Pemilik rantai (LockTaskManager.deferredLogOnReject) mengambil nomor
 * generasi saat memulai poll; setiap tick/callback hanya boleh bereaksi
 * bila [isCurrent] masih true. Rantai lama yang bangun kesiangan langsung
 * berhenti tanpa melaporkan apa pun.
 */
class PollChainGuard {

    private var generation = 0

    /** Mulai rantai baru; semua rantai sebelumnya otomatis menjadi stale. */
    @Synchronized
    fun newChain(): Int = ++generation

    /** True bila [generation] milik rantai yang masih berlaku. */
    @Synchronized
    fun isCurrent(chainGeneration: Int): Boolean = chainGeneration == generation
}
