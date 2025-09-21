#!/usr/bin/env zsh

setopt errexit
setopt nounset

local script_dir=$(grealpath $(gdirname "${(%):-%x}"))

#################
# install theme #
#################

theme_file="${ZSH_CUSTOM}/themes/pgedge-cp-env.zsh-theme"

if [[ ! -e "${theme_file}" ]]; then
    ln -s "${script_dir}/pgedge-cp-env.zsh-theme" "${theme_file}"
fi

##################
# install plugin #
##################

plugin_dir="${ZSH_CUSTOM}/plugins/pgedge-cp-env"
plugin_file="${ZSH_CUSTOM}/plugins/pgedge-cp-env/pgedge-cp-env.plugin.zsh"

mkdir -p "${plugin_dir}"

if [[ ! -e "${plugin_file}" ]]; then
    ln -s "${script_dir}/pgedge-cp-env.plugin.zsh" "${plugin_file}"
fi
