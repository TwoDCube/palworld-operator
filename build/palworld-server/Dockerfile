# Palworld dedicated server image, built to run under OpenShift's restricted-v2
# SCC (arbitrary, non-root UID). SteamCMD and the server run as an arbitrary UID
# that is always a member of the root group (GID 0); every writable path is
# group-root writable and nss_wrapper supplies a synthetic passwd entry so tools
# that expect a real user (SteamCMD) keep working.
FROM debian:bookworm-slim

# SteamCMD requires 32-bit libraries even though the Palworld server is 64-bit.
RUN dpkg --add-architecture i386 \
    && apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        jq \
        procps \
        tar \
        gzip \
        xz-utils \
        tini \
        rclone \
        libnss-wrapper \
        lib32gcc-s1 \
        lib32stdc++6 \
        libc6-i386 \
        libsdl2-2.0-0:i386 \
        locales \
    && sed -i 's/# en_US.UTF-8/en_US.UTF-8/' /etc/locale.gen \
    && locale-gen \
    && rm -rf /var/lib/apt/lists/*

ENV LANG=en_US.UTF-8 \
    LANGUAGE=en_US:en \
    LC_ALL=en_US.UTF-8

# STEAMAPPDIR is where the server binaries + world data live; it is expected to
# be backed by a PersistentVolume so restarts and updates are fast and durable.
ENV STEAM_APP_ID=2394010 \
    STEAMAPPDIR=/palworld \
    HOME=/home/steam \
    NSS_WRAPPER_PASSWD=/tmp/passwd \
    NSS_WRAPPER_GROUP=/tmp/group

# Create the layout with group-root ownership and group-write so an arbitrary
# UID (member of GID 0) can read/write everything it needs.
RUN mkdir -p ${STEAMAPPDIR} ${HOME} /steamcmd /config \
    && curl -sSL https://steamcdn-a.akamaihd.net/client/installer/steamcmd_linux.tar.gz \
        | tar -xz -C /steamcmd \
    && chgrp -R 0 ${STEAMAPPDIR} ${HOME} /steamcmd /config \
    && chmod -R g=u ${STEAMAPPDIR} ${HOME} /steamcmd /config /etc/passwd /etc/group \
    && find ${STEAMAPPDIR} ${HOME} /steamcmd /config -type d -exec chmod g+s {} +

COPY entrypoint.sh /usr/local/bin/entrypoint.sh
COPY graceful-shutdown.sh /usr/local/bin/graceful-shutdown.sh
COPY healthcheck.sh /usr/local/bin/healthcheck.sh
COPY backup.sh /usr/local/bin/backup.sh
COPY restore.sh /usr/local/bin/restore.sh
RUN chmod +x /usr/local/bin/*.sh

# Game (UDP), Steam query (UDP), RCON (TCP), REST API (TCP).
EXPOSE 8211/udp 27015/udp 25575/tcp 8212/tcp

WORKDIR ${STEAMAPPDIR}

# Run as an unprivileged, non-root default UID. OpenShift overrides this with an
# arbitrary UID from the namespace range; the entrypoint tolerates both.
USER 10000:0

# tini reaps zombies and forwards signals so graceful shutdown works.
ENTRYPOINT ["/usr/bin/tini", "-g", "--", "/usr/local/bin/entrypoint.sh"]
