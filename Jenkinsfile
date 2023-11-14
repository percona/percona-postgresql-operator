GKERegion='us-central1-c'

void CreateCluster(String CLUSTER_SUFFIX) {
    withCredentials([string(credentialsId: 'GCP_PROJECT_ID', variable: 'GCP_PROJECT'), file(credentialsId: 'gcloud-key-file', variable: 'CLIENT_SECRET_FILE')]) {
        sh """
            export KUBECONFIG=/tmp/$CLUSTER_NAME-${CLUSTER_SUFFIX}
            ret_num=0
            while [ \${ret_num} -lt 15 ]; do
                ret_val=0
                gcloud auth activate-service-account --key-file $CLIENT_SECRET_FILE
                gcloud config set project $GCP_PROJECT
                gcloud container clusters create --zone=${GKERegion} $CLUSTER_NAME-${CLUSTER_SUFFIX} --cluster-version=1.24 --machine-type=n1-standard-4 --preemptible --num-nodes=3 --network=jenkins-pg-vpc --subnetwork=jenkins-pg-${CLUSTER_SUFFIX} --no-enable-autoupgrade --cluster-ipv4-cidr=/21 --labels delete-cluster-after-hours=6 && \
                kubectl create clusterrolebinding cluster-admin-binding --clusterrole cluster-admin --user jenkins@"$GCP_PROJECT".iam.gserviceaccount.com || ret_val=\$?
                if [ \${ret_val} -eq 0 ]; then break; fi
                ret_num=\$((ret_num + 1))
            done
            if [ \${ret_num} -eq 15 ]; then
                gcloud container clusters list --filter $CLUSTER_NAME-${CLUSTER_SUFFIX} --zone $GKERegion --format='csv[no-heading](name)' | xargs gcloud container clusters delete --zone $GKERegion --quiet || true
                exit 1
            fi
        """
   }
}

void ShutdownCluster(String CLUSTER_SUFFIX) {
    withCredentials([string(credentialsId: 'GCP_PROJECT_ID', variable: 'GCP_PROJECT'), file(credentialsId: 'gcloud-key-file', variable: 'CLIENT_SECRET_FILE')]) {
        sh """
            export KUBECONFIG=/tmp/$CLUSTER_NAME-${CLUSTER_SUFFIX}
            gcloud auth activate-service-account --key-file $CLIENT_SECRET_FILE
            gcloud config set project $GCP_PROJECT
            gcloud container clusters delete --zone $GKERegion $CLUSTER_NAME-${CLUSTER_SUFFIX}
        """
   }
}

void DeleteOldClusters(String FILTER) {
    withCredentials([string(credentialsId: 'GCP_PROJECT_ID', variable: 'GCP_PROJECT'), file(credentialsId: 'gcloud-key-file', variable: 'CLIENT_SECRET_FILE')]) {
        sh """
            if gcloud --version > /dev/null 2>&1; then
                gcloud auth activate-service-account --key-file $CLIENT_SECRET_FILE
                gcloud config set project $GCP_PROJECT
                for GKE_CLUSTER in \$(gcloud container clusters list --format='csv[no-heading](name)' --filter="$FILTER"); do
                    GKE_CLUSTER_STATUS=\$(gcloud container clusters list --format='csv[no-heading](status)' --filter="\$GKE_CLUSTER")
                    retry=0
                    while [ "\$GKE_CLUSTER_STATUS" == "PROVISIONING" ]; do
                        echo "Cluster \$GKE_CLUSTER is being provisioned, waiting before delete."
                        sleep 10
                        GKE_CLUSTER_STATUS=\$(gcloud container clusters list --format='csv[no-heading](status)' --filter="\$GKE_CLUSTER")
                        let retry+=1
                        if [ \$retry -ge 60 ]; then
                            echo "Cluster \$GKE_CLUSTER to delete is being provisioned for too long. Skipping..."
                            break
                        fi
                    done
                    gcloud container clusters delete --async --zone $GKERegion --quiet \$GKE_CLUSTER || true
                done
            fi
        """
   }
}

void pushLogFile(String FILE_NAME) {
    LOG_FILE_PATH="e2e-tests/logs/${FILE_NAME}.log"
    LOG_FILE_NAME="${FILE_NAME}.log"
    echo "Push logfile $LOG_FILE_NAME file to S3!"
    withCredentials([[$class: 'AmazonWebServicesCredentialsBinding', accessKeyVariable: 'AWS_ACCESS_KEY_ID', credentialsId: 'AMI/OVF', secretKeyVariable: 'AWS_SECRET_ACCESS_KEY']]) {
        sh """
            S3_PATH=s3://percona-jenkins-artifactory-public/\$JOB_NAME/\$(git rev-parse --short HEAD)
            aws s3 ls \$S3_PATH/${LOG_FILE_NAME} || :
            aws s3 cp --content-type text/plain --quiet ${LOG_FILE_PATH} \$S3_PATH/${LOG_FILE_NAME} || :
        """
    }
}

void pushArtifactFile(String FILE_NAME) {
    echo "Push $FILE_NAME file to S3!"

    withCredentials([[$class: 'AmazonWebServicesCredentialsBinding', accessKeyVariable: 'AWS_ACCESS_KEY_ID', credentialsId: 'AMI/OVF', secretKeyVariable: 'AWS_SECRET_ACCESS_KEY']]) {
        sh """
            touch ${FILE_NAME}
            S3_PATH=s3://percona-jenkins-artifactory/\$JOB_NAME/\$(git rev-parse --short HEAD)
            aws s3 ls \$S3_PATH/${FILE_NAME} || :
            aws s3 cp --quiet ${FILE_NAME} \$S3_PATH/${FILE_NAME} || :
        """
    }
}

void popArtifactFile(String FILE_NAME) {
    echo "Try to get $FILE_NAME file from S3!"

    withCredentials([[$class: 'AmazonWebServicesCredentialsBinding', accessKeyVariable: 'AWS_ACCESS_KEY_ID', credentialsId: 'AMI/OVF', secretKeyVariable: 'AWS_SECRET_ACCESS_KEY']]) {
        sh """
            S3_PATH=s3://percona-jenkins-artifactory/\$JOB_NAME/\$(git rev-parse --short HEAD)
            aws s3 cp --quiet \$S3_PATH/${FILE_NAME} ${FILE_NAME} || :
        """
    }
}

TestsReport = '| Test name  | Status |\r\n| ------------- | ------------- |'
testsReportMap  = [:]
testsResultsMap = [:]

void makeReport() {
    def wholeTestAmount=sh(script: "ls -l e2e-tests/| grep ^d| egrep -v 'conf|data-migration-gcs|license|logs' | wc -l", , returnStdout: true).trim()
    def startedTestAmount = testsReportMap.size()
    for ( test in testsReportMap ) {
        TestsReport = TestsReport + "\r\n| ${test.key} | ${test.value} |"
    }
    TestsReport = TestsReport + "\r\n| We run $startedTestAmount out of $wholeTestAmount|"
}

void setTestsresults() {
    testsResultsMap.each { file ->
        pushArtifactFile("${file.key}")
    }
}

void runTest(String TEST_NAME, String CLUSTER_SUFFIX) {
    def retryCount = 0
    waitUntil {
        def testUrl = "https://percona-jenkins-artifactory-public.s3.amazonaws.com/cloud-pg-operator/${env.GIT_BRANCH}/${env.GIT_SHORT_COMMIT}/${TEST_NAME}.log"
        try {
            echo "The $TEST_NAME test was started!"
            testsReportMap[TEST_NAME] = "[failed]($testUrl)"
            popArtifactFile("${env.GIT_BRANCH}-${env.GIT_SHORT_COMMIT}-$TEST_NAME")

            timeout(time: 120, unit: 'MINUTES') {
                sh """
                    if [ -f "${env.GIT_BRANCH}-${env.GIT_SHORT_COMMIT}-$TEST_NAME" ]; then
                        echo Skip $TEST_NAME test
                    else
                        export KUBECONFIG=/tmp/$CLUSTER_NAME-${CLUSTER_SUFFIX}
                        ./e2e-tests/$TEST_NAME/run
                    fi
                """
            }
            testsReportMap[TEST_NAME] = "[passed]($testUrl)"
            testsResultsMap["${env.GIT_BRANCH}-${env.GIT_SHORT_COMMIT}-$TEST_NAME"] = 'passed'
            return true
        }
        catch (exc) {
            if (retryCount >= 2) {
                currentBuild.result = 'FAILURE'
                return true
            }
            retryCount++
            return false
        }
        finally {
            pushLogFile(TEST_NAME)
            echo "The $TEST_NAME test was finished!"
        }
    }
}


def skipBranchBuilds = true
if ( env.CHANGE_URL ) {
    skipBranchBuilds = false
}
def FILES_CHANGED = null
def IMAGE_EXISTS = null
def PREVIOUS_IMAGE_EXISTS = null

pipeline {
    environment {
        CLEAN_NAMESPACE = 1
        CLOUDSDK_CORE_DISABLE_PROMPTS = 1
        GIT_SHORT_COMMIT = sh(script: 'git describe --always --dirty', , returnStdout: true).trim()
        GIT_PREV_SHORT_COMMIT = sh(script: 'git describe --always HEAD~1', , returnStdout: true).trim()
        VERSION = "${env.GIT_BRANCH}-${env.GIT_SHORT_COMMIT}"
        CLUSTER_NAME = sh(script: "echo jen-pg-${env.CHANGE_ID}-${GIT_SHORT_COMMIT}-${env.BUILD_NUMBER} | tr '[:upper:]' '[:lower:]'", , returnStdout: true).trim()
        PGO_K8S_NAME = "${env.CLUSTER_NAME}-upstream"
        AUTHOR_NAME  = sh(script: "echo ${CHANGE_AUTHOR_EMAIL} | awk -F'@' '{print \$1}'", , returnStdout: true).trim()
        ECR = "119175775298.dkr.ecr.us-east-1.amazonaws.com"
        ENABLE_LOGGING="true"
    }
    agent {
        label 'docker'
    }
    options {
        disableConcurrentBuilds(abortPrevious: true)
    }
    stages {
        stage('Prepare') {
            when {
                expression {
                    !skipBranchBuilds
                }
            }
            steps {
                script {
                    if ( AUTHOR_NAME == 'null' )  {
                        AUTHOR_NAME = sh(script: "git show -s --pretty=%ae | awk -F'@' '{print \$1}'", , returnStdout: true).trim()
                    }
                    for (comment in pullRequest.comments) {
                        println("Author: ${comment.user}, Comment: ${comment.body}")
                        if (comment.user.equals('JNKPercona')) {
                            println("delete comment")
                            comment.delete()
                        }
                    }
                }
                sh '''
                    sudo curl -s -L -o /usr/local/bin/kubectl https://dl.k8s.io/release/\$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl && sudo chmod +x /usr/local/bin/kubectl
                    kubectl version --client --output=yaml

                    curl -fsSL https://raw.githubusercontent.com/helm/helm/master/scripts/get-helm-3 | bash
                    curl -s -L https://github.com/openshift/origin/releases/download/v3.11.0/openshift-origin-client-tools-v3.11.0-0cbc58b-linux-64bit.tar.gz \
                        | sudo tar -C /usr/local/bin --strip-components 1 --wildcards -zxvpf - '*/oc'

                    curl -s -L https://github.com/mitchellh/golicense/releases/latest/download/golicense_0.2.0_linux_x86_64.tar.gz \
                        | sudo tar -C /usr/local/bin --wildcards -zxvpf -

                    sudo sh -c "curl -s -L https://github.com/mikefarah/yq/releases/download/3.3.2/yq_linux_amd64 > /usr/local/bin/yq"
                    sudo chmod +x /usr/local/bin/yq

                    sudo tee /etc/yum.repos.d/google-cloud-sdk.repo << EOF
[google-cloud-cli]
name=Google Cloud CLI
baseurl=https://packages.cloud.google.com/yum/repos/cloud-sdk-el7-x86_64
enabled=1
gpgcheck=1
repo_gpgcheck=0
gpgkey=https://packages.cloud.google.com/yum/doc/rpm-package-key.gpg
EOF
                    sudo yum install -y google-cloud-cli google-cloud-cli-gke-gcloud-auth-plugin

                    sudo yum install -y jq
                '''
                script {
                    FILES_CHANGED = sh(script: "git diff --name-only HEAD HEAD~1 | grep -Ev 'e2e-tests|Jenkinsfile' | head -1", , returnStdout: true).trim() ?: null
                    IMAGE_EXISTS = sh(script: '[[ $(curl -s https://registry.hub.docker.com/v1/repositories/perconalab/percona-postgresql-operator/tags | jq -r \'.[].name\' | grep $VERSION | wc -l | grep -Eo \'[0-9]+\') == 6 ]] && echo true || : ', , returnStdout: true).trim() ?: null
                    PREVIOUS_IMAGE_EXISTS = sh(script: '[[ $(curl -s https://registry.hub.docker.com/v1/repositories/perconalab/percona-postgresql-operator/tags | jq -r \'.[].name\' | grep $GIT_BRANCH-$GIT_PREV_SHORT_COMMIT | wc -l | grep -Eo \'[0-9]+\') == 6 ]] && echo true || :  ', , returnStdout: true).trim() ?: null
                }
                withCredentials([file(credentialsId: 'cloud-secret-file', variable: 'CLOUD_SECRET_FILE'), file(credentialsId: 'cloud-minio-secret-file', variable: 'CLOUD_MINIO_SECRET_FILE')]) {
                    sh '''
                        cp $CLOUD_SECRET_FILE ./e2e-tests/conf/cloud-secret.yml
                        cp $CLOUD_MINIO_SECRET_FILE ./e2e-tests/conf/cloud-secret-minio-gw.yml
                    '''
                }
                DeleteOldClusters("jen-pg-$CHANGE_ID")
            }
        }
        stage('Retag previous commit images if possible'){
            when {
                allOf {
                    expression { !skipBranchBuilds }
                    allOf {
                        expression { FILES_CHANGED == null }
                        expression { IMAGE_EXISTS == null }
                        expression { PREVIOUS_IMAGE_EXISTS != null }
                    }
                }
            }
            steps {
                withCredentials([usernamePassword(credentialsId: 'hub.docker.com', passwordVariable: 'PASS', usernameVariable: 'USER'), [$class: 'AmazonWebServicesCredentialsBinding', accessKeyVariable: 'AWS_ACCESS_KEY_ID', credentialsId: 'AMI/OVF', secretKeyVariable: 'AWS_SECRET_ACCESS_KEY']]) {
                    sh '''
                        URI_BASE=perconalab/percona-postgresql-operator:$VERSION
                        docker_uri_base_file='./results/docker/URI_BASE'
                        mkdir -p $(dirname ${docker_uri_base_file})
                        echo ${URI_BASE} > "${docker_uri_base_file}"
                            sg docker -c "
                                docker login -u '${USER}' -p '${PASS}'
                                aws ecr get-login-password --region us-east-1 | docker login --username AWS --password-stdin $ECR
                                export IMAGE=\$URI_BASE

                                for app in "pgo-apiserver" "pgo-event" "pgo-rmdata" "pgo-scheduler" "postgres-operator" "pgo-deployer"; do
                                    docker pull perconalab/percona-postgresql-operator:$GIT_BRANCH-$GIT_PREV_SHORT_COMMIT-\\${app}
                                    docker tag perconalab/percona-postgresql-operator:$GIT_BRANCH-$GIT_PREV_SHORT_COMMIT-\\${app} \\${IMAGE}-\\${app}
                                    docker tag perconalab/percona-postgresql-operator:$GIT_BRANCH-$GIT_PREV_SHORT_COMMIT-\\${app} $ECR/\\${IMAGE}-\\${app}
                                    docker push \\${IMAGE}-\\${app}
                                    docker push $ECR/\\${IMAGE}-\\${app}
                                done
                                docker logout
                            "
                        sudo rm -rf ./build
                    '''
                }
                stash includes: 'results/docker/URI_BASE', name: 'URI_BASE'
                archiveArtifacts 'results/docker/URI_BASE'
            }
        }
        stage('Build docker image') {
            when {
                allOf {
                    expression { !skipBranchBuilds }
                    anyOf {
                        allOf {
                            expression { FILES_CHANGED != null }
                            expression { IMAGE_EXISTS == null }
                        }
                        allOf {
                            expression { FILES_CHANGED == null }
                            expression { IMAGE_EXISTS == null }
                            expression { PREVIOUS_IMAGE_EXISTS == null }
                        }
                    }
                }
            }
            steps {
                withCredentials([usernamePassword(credentialsId: 'hub.docker.com', passwordVariable: 'PASS', usernameVariable: 'USER'),[$class: 'AmazonWebServicesCredentialsBinding', accessKeyVariable: 'AWS_ACCESS_KEY_ID', credentialsId: 'AMI/OVF', secretKeyVariable: 'AWS_SECRET_ACCESS_KEY']]) {
                    sh '''
                        URI_BASE=perconalab/percona-postgresql-operator:$VERSION
                        docker_uri_base_file='./results/docker/URI_BASE'
                        mkdir -p $(dirname ${docker_uri_base_file})
                        echo ${URI_BASE} > "${docker_uri_base_file}"
                            sg docker -c "
                                docker login -u '${USER}' -p '${PASS}'
                                aws ecr get-login-password --region us-east-1 | docker login --username AWS --password-stdin $ECR
                                export IMAGE=\$URI_BASE
                                ./e2e-tests/build
                                docker logout
                            "
                        sudo rm -rf ./build
                    '''
                }
                stash includes: 'results/docker/URI_BASE', name: 'URI_BASE'
                archiveArtifacts 'results/docker/URI_BASE'
            }
        }
        stage('Save dummy URI_BASE') {
            when {
                allOf {
                    expression { !skipBranchBuilds }
                    expression { IMAGE_EXISTS != null }
                }
            }
            steps {
                sh '''
                        URI_BASE=perconalab/percona-postgresql-operator:$VERSION
                        docker_uri_base_file='./results/docker/URI_BASE'
                        mkdir -p $(dirname ${docker_uri_base_file})
                        echo ${URI_BASE} > "${docker_uri_base_file}"
                        sudo rm -rf ./build
                    '''
                stash includes: 'results/docker/URI_BASE', name: 'URI_BASE'
                archiveArtifacts 'results/docker/URI_BASE'
            }
        }
        stage('GoLicenseDetector test') {
            when {
                expression {
                    !skipBranchBuilds
                }
            }
            steps {
                sh """
                    mkdir -p $WORKSPACE/src/github.com/percona
                    ln -s $WORKSPACE $WORKSPACE/src/github.com/percona/percona-postgresql-operator
                    sg docker -c "
                        docker run \
                            --rm \
                            -v $WORKSPACE/src/github.com/percona/percona-postgresql-operator:/go/src/github.com/percona/percona-postgresql-operator \
                            -w /go/src/github.com/percona/percona-postgresql-operator \
                            -e GO111MODULE=on \
                            golang:1.18 sh -c '
                                go install github.com/google/go-licenses@latest;
                                /go/bin/go-licenses csv github.com/percona/percona-postgresql-operator/cmd/apiserver \
                                    | cut -d , -f 3 \
                                    | sort -u \
                                    > apiserver-licenses-new || :
                                /go/bin/go-licenses csv github.com/percona/percona-postgresql-operator/cmd/pgo-rmdata \
                                    | cut -d , -f 3 \
                                    | sort -u \
                                    > pgo-rmdata-licenses-new || :
                                /go/bin/go-licenses csv github.com/percona/percona-postgresql-operator/cmd/pgo-scheduler \
                                    | cut -d , -f 3 \
                                    | sort -u \
                                    > pgo-scheduler-licenses-new || :
                                /go/bin/go-licenses csv github.com/percona/percona-postgresql-operator/cmd/postgres-operator \
                                    | cut -d , -f 3 \
                                    | sort -u \
                                    > postgres-operator-licenses-new || :
                            '
                    "
                    diff -u e2e-tests/license/compare/apiserver-licenses apiserver-licenses-new
                    diff -u e2e-tests/license/compare/pgo-rmdata-licenses pgo-rmdata-licenses-new
                    diff -u e2e-tests/license/compare/pgo-scheduler-licenses pgo-scheduler-licenses-new
                    diff -u e2e-tests/license/compare/postgres-operator-licenses postgres-operator-licenses-new
                 """
            }
        }
        stage('GoLicense test') {
            when {
                expression {
                    !skipBranchBuilds
                }
            }
            steps {
                sh '''
                    mkdir -p $WORKSPACE/src/github.com/percona
                    ln -s $WORKSPACE $WORKSPACE/src/github.com/percona/percona-postgresql-operator
                    sg docker -c "
                        docker run \
                            --rm \
                            -v $WORKSPACE/src/github.com/percona/percona-postgresql-operator:/go/src/github.com/percona/percona-postgresql-operator \
                            -w /go/src/github.com/percona/percona-postgresql-operator \
                            -e GO111MODULE=on \
                            golang:1.18 sh -c 'go build -v -o apiserver github.com/percona/percona-postgresql-operator/cmd/apiserver;
                                               go build -v -o pgo-rmdata github.com/percona/percona-postgresql-operator/cmd/pgo-rmdata;
                                               go build -v -o pgo-scheduler github.com/percona/percona-postgresql-operator/cmd/pgo-scheduler;
                                               go build -v -o postgres-operator github.com/percona/percona-postgresql-operator/cmd/postgres-operator '
                    "
                '''

                withCredentials([string(credentialsId: 'GITHUB_API_TOKEN', variable: 'GITHUB_TOKEN')]) {
                    sh """
                        golicense -plain ./apiserver \
                            | grep -v 'license not found' \
                            | sed -r 's/^[^ ]+[ ]+//' \
                            | sort \
                            | uniq \
                            > apiserver-golicense-new || true
                        golicense -plain ./pgo-rmdata \
                            | grep -v 'license not found' \
                            | sed -r 's/^[^ ]+[ ]+//' \
                            | sort \
                            | uniq \
                            > pgo-rmdata-golicense-new || true
                        golicense -plain ./pgo-scheduler \
                            | grep -v 'license not found' \
                            | sed -r 's/^[^ ]+[ ]+//' \
                            | sort \
                            | uniq \
                            > pgo-scheduler-golicense-new || true
                        golicense -plain ./postgres-operator \
                            | grep -v 'license not found' \
                            | sed -r 's/^[^ ]+[ ]+//' \
                            | sort \
                            | uniq \
                            > postgres-operator-golicense-new || true

                        diff -u e2e-tests/license/compare/apiserver-golicense apiserver-golicense-new
                        diff -u e2e-tests/license/compare/pgo-rmdata-golicense pgo-rmdata-golicense-new
                        diff -u e2e-tests/license/compare/pgo-scheduler-golicense pgo-scheduler-golicense-new
                        diff -u e2e-tests/license/compare/postgres-operator-golicense postgres-operator-golicense-new
                    """
                }
            }
        }
        stage('Run tests for operator') {
            when {
                expression {
                    !skipBranchBuilds
                }
            }
            options {
                timeout(time: 5, unit: 'HOURS')
            }
            parallel {
                stage('E2E Basic tests') {
                    steps {
                        CreateCluster('sandbox')
                        withCredentials([[$class: 'AmazonWebServicesCredentialsBinding', accessKeyVariable: 'AWS_ACCESS_KEY_ID', credentialsId: 'AMI/OVF', secretKeyVariable: 'AWS_SECRET_ACCESS_KEY']]) {
                            runTest('init-deploy', 'sandbox')
                        }
                        runTest('scaling', 'sandbox')
                        runTest('recreate', 'sandbox')
                        runTest('affinity', 'sandbox')
                        runTest('monitoring', 'sandbox')
                        runTest('self-healing', 'sandbox')
                        runTest('operator-self-healing', 'sandbox')
                        runTest('clone-cluster', 'sandbox')
                        runTest('tls-check', 'sandbox')
                        runTest('users', 'sandbox')
                        runTest('ns-mode', 'sandbox')
                        ShutdownCluster('sandbox')
                    }
                }
                stage('E2E demand-backup') {
                    steps {
                        CreateCluster('demand-backup')
                        runTest('demand-backup', 'demand-backup')
                        ShutdownCluster('demand-backup')
                    }
                }
                stage('E2E scheduled-backup') {
                    steps {
                        CreateCluster('scheduled-backup')
                        runTest('scheduled-backup', 'scheduled-backup')
                        ShutdownCluster('scheduled-backup')
                    }
                }
                stage('E2E Upgrade') {
                    steps {
                        CreateCluster('upgrade')
                        runTest('upgrade', 'upgrade')
                        runTest('smart-update', 'upgrade')
                        ShutdownCluster('upgrade')
                    }
                }
                stage('E2E Version-service') {
                    steps {
                        CreateCluster('version-service')
                        runTest('version-service', 'version-service')
                        ShutdownCluster('version-service')
                    }
                }
            }
        }
    }
    post {
        always {
            script {
                setTestsresults()
                if (currentBuild.result != null && currentBuild.result != 'SUCCESS' && currentBuild.result != 'NOT_BUILT') {
                    try {
                        slackSend channel: "@${AUTHOR_NAME}", color: '#FF0000', message: "[${JOB_NAME}]: build ${currentBuild.result}, ${BUILD_URL} owner: @${AUTHOR_NAME}"
                    }
                    catch (exc) {
                        slackSend channel: '#cloud-dev-ci', color: '#FF0000', message: "[${JOB_NAME}]: build ${currentBuild.result}, ${BUILD_URL} owner: @${AUTHOR_NAME}"
                    }
                }
                if (env.CHANGE_URL) {
                    for (comment in pullRequest.comments) {
                        println("Author: ${comment.user}, Comment: ${comment.body}")
                        if (comment.user.equals('JNKPercona')) {
                            println("delete comment")
                            comment.delete()
                        }
                    }
                    if (currentBuild.result != 'NOT_BUILT') {
                        makeReport()
                        unstash 'URI_BASE'
                        def URI_BASE = sh(returnStdout: true, script: "cat results/docker/URI_BASE").trim()
                        TestsReport = TestsReport + "\r\n\r\ncommit: ${env.CHANGE_URL}/commits/${env.GIT_COMMIT}\r\nimage: `${URI_BASE}-pgo-apiserver`\r\n\r\nimage: `${URI_BASE}-pgo-event`\r\n\r\nimage: `${URI_BASE}-pgo-rmdata`\r\n\r\nimage: `${URI_BASE}-pgo-scheduler`\r\n\r\nimage: `${URI_BASE}-postgres-operator`\r\n\r\nimage: `${URI_BASE}-pgo-deployer`\r\n"
                        pullRequest.comment(TestsReport)
                    }
                }
            }
            DeleteOldClusters("$CLUSTER_NAME")
            sh """
                sudo docker system prune -fa
                sudo rm -rf ./*
                sudo rm -rf $HOME/google-cloud-sdk
            """
            deleteDir()
        }
    }
}
