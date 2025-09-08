#!/usr/bin/env python3

import json
import logging
import os
import subprocess

from dataclasses import dataclass


@dataclass
class Config:
    repo: str
    republish: bool
    dry_run: bool
    no_cache: bool
    only_postgres_version: str
    only_spock_version: str
    only_arch: str

    @staticmethod
    def from_env() -> "Config":
        # See pgedge.mk for documentation on these variables.
        return Config(
            repo=os.getenv("PGEDGE_IMAGE_REPO", "127.0.0.1:5000/pgedge-postgres"),
            republish=(os.getenv("PGEDGE_IMAGE_REPUBLISH", "0") == "1"),
            dry_run=(os.getenv("PGEDGE_IMAGE_DRY_RUN", "0") == "1"),
            no_cache=(os.getenv("PGEDGE_IMAGE_NO_CACHE", "0") == "1"),
            only_postgres_version=os.getenv("PGEDGE_IMAGE_ONLY_POSTGRES_VERSION", ""),
            only_spock_version=os.getenv("PGEDGE_IMAGE_ONLY_SPOCK_VERSION", ""),
            only_arch=os.getenv("PGEDGE_IMAGE_ONLY_ARCH", ""),
        )


@dataclass
class Tag:
    postgres_version: str
    spock_version: str = None
    epoch: int = None
    flavor: str = None

    def __str__(self) -> str:
        tag = f"{self.postgres_version}"

        if self.spock_version:
            tag += f"-spock{self.spock_version}"

        if self.flavor:
            tag += f"-{self.flavor}"

        if self.epoch:
            tag += f"-{self.epoch}"

        return tag


@dataclass
class PgEdgeImage:
    postgres_version: str
    spock_version: str
    epoch: int
    is_latest_for_pg_major: bool = False
    is_latest_for_spock_major: bool = False
    flavor: str = ""
    package_release_channel: str = ""

    @property
    def postgres_major(self) -> str:
        return self.postgres_version.split(".")[0]

    @property
    def spock_major(self) -> str:
        return self.spock_version.split(".")[0]

    @property
    def package_list(self) -> str:
        filename = f"pg{self.postgres_version}-spock{self.spock_version}"

        if self.flavor:
            filename += f"-{self.flavor}"

        return filename + ".txt"

    @property
    def build_tag(self) -> Tag:
        # Immutable tag with epoch
        return Tag(
            postgres_version=self.postgres_version,
            flavor=self.flavor,
            spock_version=self.spock_version,
            epoch=self.epoch,
        )

    @property
    def extra_tags(self) -> list[Tag]:
        tags = [
            # Mutable tag without epoch
            Tag(
                postgres_version=self.postgres_version,
                flavor=self.flavor,
                spock_version=self.spock_version,
            )
        ]

        if self.is_latest_for_spock_major:
            # Mutable tag without spock minor/patch and epoch
            tags.append(
                Tag(
                    postgres_version=self.postgres_version,
                    flavor=self.flavor,
                    spock_version=self.spock_major,
                )
            )

            if self.is_latest_for_pg_major:
                # Mutable tag without postgres major, spock minor/patch, and epoch
                tags.append(
                    Tag(
                        postgres_version=self.postgres_major,
                        flavor=self.flavor,
                        spock_version=self.spock_major,
                    )
                )

        return tags

    @property
    def all_tags(self) -> list[Tag]:
        return [self.build_tag, *self.extra_tags]


def make_all_flavor_images(
    postgres_version: str,
    spock_version: str,
    epoch: int,
    is_latest_for_pg_major: bool = False,
    is_latest_for_spock_major: bool = False,
    package_release_channel: str = "",
) -> list[PgEdgeImage]:
    images: list[PgEdgeImage] = []
    for flavor in ["minimal", "standard"]:
        images.append(
            PgEdgeImage(
                postgres_version=postgres_version,
                spock_version=spock_version,
                epoch=epoch,
                is_latest_for_pg_major=is_latest_for_pg_major,
                is_latest_for_spock_major=is_latest_for_spock_major,
                flavor=flavor,
                package_release_channel=package_release_channel,
            )
        )

    return images


# This is the list of all images that this script will build. Any new images should be
# added to this list.
all_images: list[PgEdgeImage] = [
    # pg16 images
    *make_all_flavor_images(
        postgres_version="16.10",
        spock_version="5.0.1",
        epoch=1,
        is_latest_for_pg_major=True,
        is_latest_for_spock_major=True,
    ),
    # pg17 images
    *make_all_flavor_images(
        postgres_version="17.5",
        spock_version="5.0.1",
        epoch=1,
        is_latest_for_spock_major=True,
        package_release_channel="staging",
    ),
    *make_all_flavor_images(
        postgres_version="17.6",
        spock_version="5.0.1",
        epoch=1,
        is_latest_for_pg_major=True,
        is_latest_for_spock_major=True,
    ),
]


def validate_images(images: list[PgEdgeImage]):
    all_tags = set()

    for image in images:
        image_tags = set(map(str, image.all_tags))
        overlap = all_tags.intersection(image_tags)
        if len(overlap) != 0:
            invalid = ", ".join(overlap)
            raise ValueError(f"images list produces duplicate tags: {invalid}")
        all_tags.update(image_tags)


def bake_cmd(*args: str) -> list[str]:
    return ["docker", "buildx", "bake", "--file", "pgedge.docker-bake.hcl", *args]


def imagetools_cmd(*args: str) -> list[str]:
    return ["docker", "buildx", "imagetools", *args]


def published_digests(repo: str, tag: Tag) -> set[str]:
    logging.info(f"checking repository {repo} for tag {tag}")

    try:
        out = subprocess.check_output(
            imagetools_cmd("inspect", "--raw", f"{repo}:{tag}"),
            stderr=subprocess.PIPE,
        )
        raw = json.loads(out)
        return set(
            manifest.get("digest")
            for manifest in raw.get("manifests", [])
            if manifest.get("digest") is not None
        )
    except subprocess.CalledProcessError:
        return set()


def build(
    repo: str,
    image: PgEdgeImage,
    dry_run: bool,
    no_cache: bool,
    only_arch: str,
):
    logging.info("building and pushing images")

    if dry_run:
        logging.info("skipping image build")
        return

    if repo.startswith("127.0.0.1"):
        # The buildx builder is its own container, and it doesn't share the host network
        # namespace.
        repo = repo.replace("127.0.0.1", "host.docker.internal")

    bake_args = ["--push"]
    if no_cache:
        bake_args.append("--no-cache")
    if only_arch:
        bake_args.extend(("--set", f"default.platforms=linux/{only_arch}"))

    subprocess.check_output(
        bake_cmd("--push"),
        env={
            **os.environ.copy(),
            "PACKAGE_RELEASE_CHANNEL": image.package_release_channel,
            "POSTGRES_MAJOR_VERSION": image.postgres_major,
            "PACKAGE_LIST_FILE": image.package_list,
            "TAG": f"{repo}:{image.build_tag}",
            "TARGET": image.flavor,
        },
    )


def add_tag(repo: str, existing_tag: Tag, new_tag: Tag, dry_run: bool):
    logging.info(
        f"adding new tag {new_tag} to existing manifest with tag {existing_tag}"
    )

    if dry_run:
        logging.info("skipping tag creation")
        return

    subprocess.check_output(
        imagetools_cmd("create", "--tag", f"{repo}:{new_tag}", f"{repo}:{existing_tag}")
    )


def main():
    logging.basicConfig(
        level=logging.INFO,
        format="%(levelname)s: %(message)s",
    )

    config = Config.from_env()

    if config.dry_run:
        logging.info("dry run enabled. build and publish actions will be skipped.")

    if config.republish:
        logging.info("republish enabled. images will be republished.")

    if config.no_cache:
        logging.info("no cache enabled. images will be built without cache.")

    if config.only_postgres_version:
        logging.info(
            f"only postgres {config.only_postgres_version} enabled. other images will be skipped."
        )

    if config.only_spock_version:
        logging.info(
            f"only spock {config.only_spock_version} enabled. other images will be skipped."
        )

    if config.only_arch:
        logging.info(
            f"only arch {config.only_arch} enabled. other images will be skipped."
        )

    validate_images(all_images)

    for image in all_images:
        if (
            config.only_postgres_version
            and image.postgres_version != config.only_postgres_version
        ) or (
            config.only_spock_version
            and image.spock_version != config.only_spock_version
        ):
            logging.info(f"skipping image {image.build_tag}")
            continue

        published = published_digests(config.repo, image.build_tag)
        if len(published) == 0 or config.republish:
            build(
                repo=config.repo,
                image=image,
                dry_run=config.dry_run,
                no_cache=config.no_cache,
                only_arch=config.only_arch,
            )
            published = published_digests(config.repo, image.build_tag)
        else:
            logging.info(f"{image.build_tag} is already published")

        for tag in image.extra_tags:
            tag_published = published_digests(config.repo, tag)
            if (
                len(tag_published) == 0
                or not tag_published.issubset(published)
                or config.republish
            ):
                add_tag(
                    repo=config.repo,
                    existing_tag=image.build_tag,
                    new_tag=tag,
                    dry_run=config.dry_run,
                )
            else:
                logging.info(f"{tag} is already up-to-date")


if __name__ == "__main__":
    main()
