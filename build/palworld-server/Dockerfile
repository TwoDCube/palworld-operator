# Palworld dedicated server image, built on UBI9 to run under OpenShift's
# restricted-v2 SCC (arbitrary, non-root UID). SteamCMD and the server run as an
# arbitrary UID that is always a member of the root group (GID 0); every writable
# path is group-root writable, and the entrypoint appends a passwd entry (via a
# group-writable /etc/passwd) so tools that expect a real user keep working.
FROM registry.access.redhat.com/ubi9/ubi:latest

# SteamCMD requires 32-bit glibc/libstdc++ even though the Palworld server is
# 64-bit. `tini` and `rclone` are not in the UBI repos, so they are fetched as
# static binaries.
ARG TINI_VERSION=v0.19.0
RUN dnf -y install \
        glibc.i686 \
        libstdc++.i686 \
        ca-certificates \
        tar \
        gzip \
        xz \
        procps-ng \
        findutils \
        gawk \
        jq \
        unzip \
        shadow-utils \
        glibc-langpack-en \
    && dnf clean all \
    && rm -rf /var/cache/dnf \
    && curl -fsSL -o /usr/bin/tini \
        "https://github.com/krallin/tini/releases/download/${TINI_VERSION}/tini-static-amd64" \
    && chmod +x /usr/bin/tini \
    && curl -fsSL -o /tmp/rclone.zip \
        "https://downloads.rclone.org/rclone-current-linux-amd64.zip" \
    && unzip -j /tmp/rclone.zip '*/rclone' -d /usr/bin \
    && chmod +x /usr/bin/rclone \
    && rm -f /tmp/rclone.zip

ENV LANG=en_US.UTF-8 \
    LC_ALL=en_US.UTF-8

# STEAMAPPDIR is where the server binaries + world data live; it is expected to
# be backed by a PersistentVolume so restarts and updates are fast and durable.
ENV STEAM_APP_ID=2394010 \
    STEAMAPPDIR=/palworld \
    HOME=/home/steam

# Create the layout with group-root ownership and group-write so an arbitrary
# UID (member of GID 0) can read/write everything it needs, and make /etc/passwd
# group-writable so the entrypoint can register the arbitrary UID.
RUN mkdir -p ${STEAMAPPDIR} ${HOME} /steamcmd /config \
    && curl -sSL https://steamcdn-a.akamaihd.net/client/installer/steamcmd_linux.tar.gz \
        | tar -xz -C /steamcmd \
    && chgrp -R 0 ${STEAMAPPDIR} ${HOME} /steamcmd /config \
    && chmod -R g=u ${STEAMAPPDIR} ${HOME} /steamcmd /config \
    && find ${STEAMAPPDIR} ${HOME} /steamcmd /config -type d -exec chmod g+s {} + \
    && chmod g=u /etc/passwd /etc/group

COPY entrypoint.sh /usr/local/bin/entrypoint.sh
COPY graceful-shutdown.sh /usr/local/bin/graceful-shutdown.sh
COPY healthcheck.sh /usr/local/bin/healthcheck.sh
COPY backup.sh /usr/local/bin/backup.sh
COPY restore.sh /usr/local/bin/restore.sh
RUN chmod +x /usr/local/bin/*.sh

LABEL name="palworld-server" \
      vendor="twodcube" \
      summary="Palworld dedicated server (OpenShift arbitrary-UID compatible)" \
      io.openshift.tags="palworld,game-server"

# Game (UDP), Steam query (UDP), RCON (TCP), REST API (TCP).
EXPOSE 8211/udp 27015/udp 25575/tcp 8212/tcp

WORKDIR ${STEAMAPPDIR}

# Run as an unprivileged, non-root default UID. OpenShift overrides this with an
# arbitrary UID from the namespace range; the entrypoint tolerates both.
USER 10000:0

# tini reaps zombies and forwards signals so graceful shutdown works.
ENTRYPOINT ["/usr/bin/tini", "-g", "--", "/usr/local/bin/entrypoint.sh"]
