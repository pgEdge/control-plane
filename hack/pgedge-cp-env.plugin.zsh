##############################
# overridable theme settings #
##############################

ZSH_THEME_PGEDGE_CP_ENV_PROMPT_PREFIX='cp-env:('
ZSH_THEME_PGEDGE_CP_ENV_PROMPT_SUFFIX=')'

ZSH_THEME_PGEDGE_CP_ENV_PROMPT_COMPOSE_STYLE="%{$fg_bold[green]%}"
ZSH_THEME_PGEDGE_CP_ENV_PROMPT_LIMA_STYLE="%{$fg_bold[cyan]%}"
ZSH_THEME_PGEDGE_CP_ENV_PROMPT_EC2_STYLE="%{$fg_bold[yellow]%}"
ZSH_THEME_PGEDGE_CP_ENV_PROMPT_OTHER_STYLE="%{$fg_bold[magenta]%}"

###################
# prompt function #
###################

pgedge_cp_env_prompt_info() {
    if [[ -z "${CP_ENV}" ]]; then
        return
    fi

    local style

    case "${CP_ENV}" in
        compose)
            style="${ZSH_THEME_PGEDGE_CP_ENV_PROMPT_COMPOSE_STYLE}"
            ;;
        lima)
            style="${ZSH_THEME_PGEDGE_CP_ENV_PROMPT_LIMA_STYLE}"
            ;;
        ec2)
            style="${ZSH_THEME_PGEDGE_CP_ENV_PROMPT_EC2_STYLE}"
            ;;
        *)
            style="${ZSH_THEME_PGEDGE_CP_ENV_PROMPT_OTHER_STYLE}"
            ;;
    esac

    local prefix="${ZSH_THEME_PGEDGE_CP_ENV_PROMPT_PREFIX}"
    local suffix="${ZSH_THEME_PGEDGE_CP_ENV_PROMPT_SUFFIX}"

    echo "%{$fg_no_bold[default]%}${prefix}${style}${CP_ENV}%{$reset_color%}${suffix}"
}
